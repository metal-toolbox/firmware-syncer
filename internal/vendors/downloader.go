package vendors

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/metal-toolbox/firmware-syncer/internal/config"

	rcloneLocal "github.com/rclone/rclone/backend/local"
	rcloneS3 "github.com/rclone/rclone/backend/s3"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneConfigmap "github.com/rclone/rclone/fs/config/configmap"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
)

var (
	ErrDestPathUndefined  = errors.New("destination path is not specified")
	ErrCopy               = errors.New("error copying files")
	ErrSync               = errors.New("error syncing files")
	ErrInitS3Downloader   = errors.New("error intializing s3 downloader")
	ErrInitHTTPDownloader = errors.New("error initializing http downloader")
	ErrInitFSDownloader   = errors.New("error initializing filesystem downloader")

	ErrFileStoreConfig      = errors.New("filestore configuration invalid")
	ErrRootDirUndefined     = errors.New("expected a root directory path to mount")
	ErrInitS3Fs             = errors.New("error initializing s3 vfs")
	ErrUnsupportedFileStore = errors.New("unsupported file store")
	ErrSourceURL            = errors.New("invalid/unsupported source URL")
	ErrStoreConfig          = errors.New("error in/invalid FileStore configuration")
	ErrURLUnsupported       = errors.New("error URL scheme/format unsupported")

	ErrFileNotFound    = errors.New("file not found")
	ErrCheckFileExists = errors.New("error checking file exists")
	ErrListingFiles    = errors.New("error listing files in directory")
	ErrDirEmpty        = errors.New("directory empty")
	ErrModTimeFile     = errors.New("error retrieving file mod time")
	ErrCreatingTmpDir  = errors.New("error creating tmp dir")

	ErrUnexpectedStatusCode = errors.New("unexpected status code")
	ErrDownloadingFile      = errors.New("failed to download file")
)

//go:generate mockgen -source=downloader.go -destination=mocks/downloader.go Downloader

// Downloader is something that can download a file for a given firmware.
type Downloader interface {
	// Download takes in the directory to download the file to, and the firmware to be downloaded.
	// It should also return the full path to the downloaded file.
	Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error)
}

// DownloaderStats includes fields for stats on file/object transfer for Downloader
type DownloaderStats struct {
	BytesTransferred   int64
	ObjectsTransferred int64
	Errors             int64
}

// LocalFsConfig for the downloader
type LocalFsConfig struct {
	Root string
}

func SetRcloneLogging(logger *logrus.Logger) {
	switch logger.GetLevel() {
	case logrus.DebugLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
	case logrus.TraceLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
		_ = rcloneFs.GetConfig(context.Background()).Dump.Set("headers")
	}
}

func SrcPath(fw *serverservice.ComponentFirmwareVersion) string {
	u, _ := url.Parse(fw.UpstreamURL)
	return u.Path
}

func DstPath(fw *serverservice.ComponentFirmwareVersion) string {
	return path.Join(fw.Vendor, fw.Filename)
}

// InitLocalFs initializes and returns a rcloneFs.Fs interface on the local filesystem
func InitLocalFs(ctx context.Context, cfg *LocalFsConfig) (rcloneFs.Fs, error) {
	if cfg == nil {
		return nil, errors.Wrap(ErrFileStoreConfig, "got nil local fs config")
	}

	if cfg.Root == "" {
		return nil, errors.Wrap(ErrRootDirUndefined, "initLocalFs")
	}

	// https://github.com/rclone/rclone/blob/master/backend/local/local.go#L40
	opts := rcloneConfigmap.Simple{
		"type":             "local",
		"copy_links":       "true",
		"no_check_updated": "false",
		"one_file_system":  "true",
		"case_sensitive":   "true",
		"no_preallocation": "true",
		"no_set_modtime":   "false",
	}

	fs, err := rcloneLocal.NewFs(ctx, "local://"+cfg.Root, cfg.Root, opts)
	if err != nil {
		return nil, errors.Wrap(ErrInitFSDownloader, err.Error())
	}

	return fs, nil
}

// InitS3Fs initializes and returns a rcloneFs.Fs interface on an s3 store
//
// root: the directory mounted as the root/top level directory of the returned fs
func InitS3Fs(ctx context.Context, cfg *config.S3Bucket, root string) (rcloneFs.Fs, error) {
	if cfg == nil {
		return nil, errors.Wrap(ErrFileStoreConfig, "got nil s3 config")
	}

	if root == "" {
		return nil, errors.Wrap(ErrRootDirUndefined, "initS3Fs")
	}

	if cfg.Region == "" {
		return nil, errors.Wrap(ErrInitS3Fs, "s3 region not defined")
	}

	if cfg.Endpoint == "" {
		return nil, errors.Wrap(ErrInitS3Fs, "s3 endpoint not defined")
	}

	if cfg.AccessKey == "" {
		return nil, errors.Wrap(ErrInitS3Fs, "s3 access key not defined")
	}

	if cfg.SecretKey == "" {
		return nil, errors.Wrap(ErrInitS3Fs, "s3 secret key not defined")
	}

	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}

	// https://github.com/rclone/rclone/blob/master/backend/s3/s3.go#L126
	opts := rcloneConfigmap.Simple{
		"type":                 "s3",
		"provider":             "AWS",
		"region":               cfg.Region,
		"access_key_id":        cfg.AccessKey,
		"secret_access_key":    cfg.SecretKey,
		"endpoint":             cfg.Endpoint,
		"leave_parts_on_error": "true",
		"disable_http2":        "true",  // https://github.com/rclone/rclone/issues/3631
		"chunk_size":           "10M",   // upload chunksize, the bytes buffered from the source before upload to destination
		"list_chunk":           "1000",  // number of objects to return in a listing
		"copy_cutoff":          "1000",  // Cutoff for switching to multipart copy
		"upload_cutoff":        "10M",   // Any files larger than this will be uploaded in chunks of chunk_size. The minimum is 0 and the maximum is 5 GiB.
		"upload_concurrency":   "5",     // This is the number of chunks of the same file that are uploaded concurrently.
		"disable_checksum":     "false", // store MD5 checksum with object metadata
		"force_path_style":     "true",
		"no_check_bucket":      "true",
		"no_head":              "true", // XXX 1.60.0 introduced s3 versions support and it issues a HEAD request with ?VersionId which causes a 403 error in our case.
	}

	mount := cfg.Bucket + root

	fs, err := rcloneS3.NewFs(ctx, "s3://"+mount, mount, opts)
	if err != nil {
		return nil, errors.Wrap(ErrInitS3Fs, err.Error())
	}

	return fs, nil
}

// SplitURLPath returns the URL host and Path parts while including the URL scheme, user info and fragments if any
func SplitURLPath(httpURL string) (hostPart, pathPart string, err error) {
	if !strings.HasPrefix(httpURL, "http://") && !strings.HasPrefix(httpURL, "https://") {
		return "", "", errors.Wrap(ErrURLUnsupported, httpURL)
	}

	u, err := url.Parse(httpURL)
	if err != nil {
		return "", "", errors.Wrap(err, httpURL)
	}

	hostPart = u.Host
	if u.User != nil {
		hostPart = u.User.String() + "@" + u.Host
	}

	hostPart = u.Scheme + "://" + hostPart

	pathPart = u.Path
	if u.RawQuery != "" {
		pathPart += "?" + u.RawQuery
	}

	return hostPart, pathPart, nil
}

// DownloadFirmwareArchive downloads a zip archive from archiveURL to tmpDir optionally checking the archive checksum
func DownloadFirmwareArchive(ctx context.Context, tmpDir, archiveURL, archiveChecksum string) (string, error) {
	zipArchivePath := path.Join(tmpDir, filepath.Base(archiveURL))

	out, err := os.Create(zipArchivePath)
	if err != nil {
		return "", err
	}

	err = rcloneOperations.CopyURLToWriter(ctx, archiveURL, out)
	if err != nil {
		return "", err
	}

	if archiveChecksum != "" {
		if !ValidateChecksum(zipArchivePath, archiveChecksum) {
			return "", errors.Wrap(ErrChecksumValidate, fmt.Sprintf("zipArchivePath: %s, expected checksum: %s", zipArchivePath, archiveChecksum))
		}
	}

	return zipArchivePath, nil
}

// ExtractFromZipArchive extracts the given firmareFilename from zip archivePath and checks if MD5 checksum matches.
// nolint:gocyclo // see Test_ExtractFromZipArchive for examples of zip archives found in the wild.
func ExtractFromZipArchive(archivePath, firmwareFilename, firmwareChecksum string) (*os.File, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var foundFile *zip.File

	fwFilenameNoExt := strings.Replace(firmwareFilename, filepath.Ext(firmwareFilename), "", 1)
	for _, f := range r.File {
		if filepath.Ext(f.Name) == ".zip" && strings.Contains(f.Name, fwFilenameNoExt) {
			foundFile = f
			// Skip checksum verification on the nested zip archive,
			// since we don't have a checksum for it.
			firmwareChecksum = ""

			break
		}

		if strings.HasSuffix(f.Name, firmwareFilename) {
			foundFile = f
			break
		}
	}

	if foundFile == nil {
		return nil, errors.New(fmt.Sprintf("couldn't find file: %s in archive: %s", firmwareFilename, archivePath))
	}

	zipContents, err := foundFile.Open()
	if err != nil {
		return nil, err
	}
	defer zipContents.Close()

	tmpDir := path.Dir(archivePath)
	tmpFilename := filepath.Base(foundFile.Name)

	out, err := os.Create(path.Join(tmpDir, tmpFilename))
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(out, zipContents)
	if err != nil {
		return nil, err
	}

	if filepath.Ext(out.Name()) == ".zip" {
		out, err = ExtractFromZipArchive(out.Name(), firmwareFilename, firmwareChecksum)
		if err != nil {
			return nil, err
		}
	}

	if firmwareChecksum != "" && !ValidateChecksum(out.Name(), firmwareChecksum) {
		return nil, errors.Wrap(ErrChecksumValidate, fmt.Sprintf("firmware: %s, expected checksum: %s", out.Name(), firmwareChecksum))
	}

	return out, nil
}

type ArchiveDownloader struct {
	logger *logrus.Logger
}

// NewArchiveDownloader creates a new ArchiveDownloader.
func NewArchiveDownloader(logger *logrus.Logger) Downloader {
	return &ArchiveDownloader{logger: logger}
}

// Download will download the file for the given firmware into the given downloadDir,
// and return the full path to the downloaded file.
func (m *ArchiveDownloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	archivePath, err := DownloadFirmwareArchive(ctx, downloadDir, firmware.UpstreamURL, "")
	if err != nil {
		return "", err
	}

	m.logger.WithField("archivePath", archivePath).Debug("Archive downloaded.")
	m.logger.Debug("Extracting firmware from archive")

	fwFile, err := ExtractFromZipArchive(archivePath, firmware.Filename, "")
	if err != nil {
		return "", err
	}

	return fwFile.Name(), nil
}

type RcloneDownloader struct {
	logger *logrus.Logger
}

// NewRcloneDownloader creates a new RcloneDownloader.
func NewRcloneDownloader(logger *logrus.Logger) Downloader {
	return &RcloneDownloader{logger: logger}
}

// Download will download the file for the given firmware into the given downloadDir,
// and return the full path to the downloaded file.
func (r *RcloneDownloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	return DownloadFirmwareArchive(ctx, downloadDir, firmware.UpstreamURL, "")
}

type S3Downloader struct {
	logger *logrus.Logger
	s3Fs   rcloneFs.Fs
}

// NewS3Downloader creats a new S3Downloader.
func NewS3Downloader(logger *logrus.Logger, s3Fs rcloneFs.Fs) Downloader {
	return &S3Downloader{logger: logger, s3Fs: s3Fs}
}

// Download will download the file for the given firmware into the given downloadDir,
// and return the full path to the downloaded file.
func (s *S3Downloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	tmpFS, err := InitLocalFs(ctx, &LocalFsConfig{Root: downloadDir})
	if err != nil {
		return "", err
	}

	err = rcloneOperations.CopyFile(ctx, tmpFS, s.s3Fs, firmware.Filename, SrcPath(firmware))
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			msg := fmt.Sprintf("%s: %s", err, firmware.Filename)
			return "", errors.Wrap(ErrCopy, msg)
		}

		return "", err
	}

	return path.Join(downloadDir, firmware.Filename), nil
}

// SourceOverrideDownloader is meant to download firmware from an alternate source
// than the firmware's UpstreamURL.
type SourceOverrideDownloader struct {
	logger  *logrus.Logger
	client  serverservice.Doer
	baseURL string
}

// NewSourceOverrideDownloader creates a SourceOverrideDownloader.
func NewSourceOverrideDownloader(logger *logrus.Logger, client serverservice.Doer, sourceURL string) Downloader {
	if !strings.HasSuffix(sourceURL, "/") {
		sourceURL += "/"
	}

	return &SourceOverrideDownloader{
		logger,
		client,
		sourceURL,
	}
}

// Download will download the given firmware into the given downloadDir,
// and return the full path to the downloaded file.
// The file will be downloaded from the sourceURL provided to the SourceOverrideDownloader
// instead of the firmware's UpstreamURL.
func (d *SourceOverrideDownloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	filePath := filepath.Join(downloadDir, firmware.Filename)

	firmwareURL, err := url.JoinPath(d.baseURL, firmware.Filename)
	if err != nil {
		return "", errors.Wrap(ErrSourceURL, err.Error())
	}

	d.logger.WithField("url", firmwareURL).
		WithField("firmware", firmware.Filename).
		WithField("vendor", firmware.Vendor).
		Info("Downloading firmware")

	file, err := os.Create(filePath)
	if err != nil {
		return "", errors.Wrap(ErrCreatingTmpDir, err.Error())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, firmwareURL, http.NoBody)
	if err != nil {
		return "", errors.Wrap(ErrSourceURL, err.Error())
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", errors.Wrap(ErrDownloadingFile, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Wrap(ErrUnexpectedStatusCode, fmt.Sprintf("status code %d", resp.StatusCode))
	}

	if _, err = io.Copy(file, resp.Body); err != nil {
		return "", errors.Wrap(ErrCopy, err.Error())
	}

	return filePath, nil
}
