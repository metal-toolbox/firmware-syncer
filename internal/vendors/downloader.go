package vendors

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/metal-toolbox/firmware-syncer/app"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	rcloneHttp "github.com/rclone/rclone/backend/http"
	rcloneLocal "github.com/rclone/rclone/backend/local"
	rcloneS3 "github.com/rclone/rclone/backend/s3"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneConfigmap "github.com/rclone/rclone/fs/config/configmap"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
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
)

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

func VerifyFile(ctx context.Context, tmpFs, srcFs rcloneFs.Fs, fw *serverservice.ComponentFirmwareVersion) error {
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(tmpFs.Root(), "verify-")
	if err != nil {
		return errors.Wrap(ErrCreatingTmpDir, err.Error())
	}

	defer os.RemoveAll(tmpDir)

	dstPath := path.Join(path.Base(tmpDir), fw.Filename)

	switch {
	case strings.HasPrefix(fw.UpstreamURL, "s3://"):
		err = rcloneOperations.CopyFile(ctx, tmpFs, srcFs, dstPath, SrcPath(fw))
	case strings.HasPrefix(fw.UpstreamURL, "http://"), strings.HasPrefix(fw.UpstreamURL, "https://"):
		_, err = rcloneOperations.CopyURL(ctx, tmpFs, dstPath, fw.UpstreamURL, false, false, false)
	}

	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+fw.Filename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	tmpFilename := path.Join(tmpFs.Root(), dstPath)

	if !ValidateChecksum(tmpFilename, fw.Checksum) {
		return errors.Wrap(ErrChecksumValidate, fmt.Sprintf("tmpFilename: %s, expected checksum: %s", tmpFilename, fw.Checksum))
	}

	return nil
}

// initHttpFs initializes and returns a rcloneFs.Fs interface that can be used for Copy, Sync operations
// the Fs is initialized based the urlHost, urlPath parameters
//
// httpURL: the http endpoint which is expected to be the root/top level directory from where files are to be copied from/to
//
//	this can be a http index or a URL endpoint from which files are to be downloaded.
func InitHTTPFs(ctx context.Context, httpURL string) (rcloneFs.Fs, error) {
	// parse the URL into host and path parts, as expected by the rclone fs lib
	hostPart, pathPart, err := SplitURLPath(httpURL)
	if err != nil {
		return nil, err
	}

	// https://github.com/rclone/rclone/blob/master/backend/http/http.go#L36
	opts := rcloneConfigmap.Simple{
		"type":    "http",
		"no_head": "true",
		"url":     hostPart,
	}

	fs, err := rcloneHttp.NewFs(ctx, httpURL, pathPart, opts)

	if err != nil && !errors.Is(err, rcloneFs.ErrorIsFile) {
		return nil, errors.Wrap(ErrInitHTTPDownloader, err.Error())
	}

	return fs, nil
}

// initLocalFs initializes and returns a rcloneFs.Fs interface on the local filesystem
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

// initS3Fs initializes and returns a rcloneFs.Fs interface on an s3 store
//
// root: the directory mounted as the root/top level directory of the returned fs
func InitS3Fs(ctx context.Context, cfg *app.S3Bucket, root string) (rcloneFs.Fs, error) {
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
