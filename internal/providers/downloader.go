package providers

import (
	"context"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/bmc-toolbox/common"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	rcloneHttp "github.com/rclone/rclone/backend/http"
	rcloneLocal "github.com/rclone/rclone/backend/local"
	rcloneS3 "github.com/rclone/rclone/backend/s3"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneStats "github.com/rclone/rclone/fs/accounting"
	rcloneConfigmap "github.com/rclone/rclone/fs/config/configmap"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
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

// Downloader wraps src and dst rclone Fs interface types to enable copying objects
type Downloader struct {
	vendor string
	// srcURL is the source URL configured for the src fs
	srcURL string
	// dstURL is the destination URL for the dst fs
	dstURL string
	// src is the remote file store
	src rcloneFs.Fs
	// dst is the destination S3 file store
	dst    rcloneFs.Fs
	dstCfg *config.S3Bucket
	// tmp is a temporary work file store
	tmp    rcloneFs.Fs
	logger *logrus.Logger
}

type S3Downloader struct {
	vendor string
	src    rcloneFs.Fs
	srcCfg *config.S3Bucket
	dst    rcloneFs.Fs
	dstCfg *config.S3Bucket
	tmp    rcloneFs.Fs
	logger *logrus.Logger
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

func NewS3Downloader(ctx context.Context, vendor string, srcCfg, dstCfg *config.S3Bucket, logger *logrus.Logger) (*S3Downloader, error) {
	var err error

	switch logger.GetLevel() {
	case logrus.DebugLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
	case logrus.TraceLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
		_ = rcloneFs.GetConfig(context.Background()).Dump.Set("headers")
	}

	downloader := &S3Downloader{
		vendor: vendor,
		srcCfg: srcCfg,
		dstCfg: dstCfg,
		logger: logger,
	}

	downloader.tmp, err = initLocalFs(ctx, &LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	downloader.src, err = initS3Fs(ctx, srcCfg, "/")
	if err != nil {
		return nil, err
	}

	downloader.dst, err = initS3Fs(ctx, dstCfg, "/")
	if err != nil {
		return nil, err
	}

	return downloader, nil
}

// CopyFile wraps rclone CopyFile to copy firmware file from src to dst
func (s *S3Downloader) CopyFile(ctx context.Context, fw *config.Firmware) error {
	var err error

	// In case the file already exists in dst, don't verify/copy it
	if exists, _ := rcloneFs.FileExists(ctx, s.dst, s.DstPath(fw)); exists {
		s.logger.WithFields(
			logrus.Fields{
				"filename": fw.Filename,
			},
		).Debug("firmware already exists at dst")

		return nil
	}

	err = s.VerifyFile(ctx, fw)
	if err != nil {
		return err
	}

	err = rcloneOperations.CopyFile(ctx, s.dst, s.src, s.DstPath(fw), s.SrcPath(fw))
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+fw.Filename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

func (s *S3Downloader) SrcBucket() string {
	return s.srcCfg.Bucket
}

func (s *S3Downloader) DstBucket() string {
	return s.dstCfg.Bucket
}

func (s *S3Downloader) SrcPath(fw *config.Firmware) string {
	u, _ := url.Parse(fw.UpstreamURL)
	return u.Path
}

func (s *S3Downloader) DstPath(fw *config.Firmware) string {
	return path.Join(
		"/firmware",
		UpdateFilesPath(
			s.vendor, fw.Model, fw.ComponentSlug, fw.Filename))
}

func (s *S3Downloader) VerifyFile(ctx context.Context, fw *config.Firmware) error {
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(s.tmp.Root(), "verify-")
	if err != nil {
		return errors.Wrap(ErrCreatingTmpDir, err.Error())
	}

	defer os.RemoveAll(tmpDir)

	dstPath := path.Join(path.Base(tmpDir), fw.Filename)

	err = rcloneOperations.CopyFile(ctx, s.tmp, s.src, dstPath, s.SrcPath(fw))
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+fw.Filename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	tmpFilename := path.Join(s.tmp.Root(), dstPath)

	return SHA256ChecksumValidate(tmpFilename, fw.FileCheckSum)
}

// NewDownloader initializes a downloader object based on the srcURL and the given dstCfg
func NewDownloader(ctx context.Context, vendor, srcURL string, dstCfg *config.S3Bucket, logger *logrus.Logger) (*Downloader, error) {
	var err error

	switch logger.GetLevel() {
	case logrus.DebugLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
	case logrus.TraceLevel:
		rcloneFs.GetConfig(context.Background()).LogLevel = rcloneFs.LogLevelDebug
		_ = rcloneFs.GetConfig(context.Background()).Dump.Set("headers")
	}

	downloader := &Downloader{
		vendor: vendor,
		srcURL: srcURL,
		dstCfg: dstCfg,
		logger: logger,
	}

	downloader.dst, err = initS3Fs(ctx, dstCfg, "/")
	if err != nil {
		return nil, err
	}

	// init local tmp fs to generate checksum and signature files
	downloader.tmp, err = initLocalFs(ctx, &LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	// init source to download files
	downloader.src, err = initSource(ctx, srcURL)
	if err != nil {
		return nil, err
	}

	return downloader, nil
}

// initSource initializes the source URL based on its URL scheme and returns a rclone.Fs with Copy/Sync methods
func initSource(ctx context.Context, srcURL string) (rcloneFs.Fs, error) {
	var err error

	var fs rcloneFs.Fs

	if srcURL == "" {
		return nil, errors.Wrap(ErrSourceURL, "got empty string")
	}

	switch {
	case strings.HasPrefix(srcURL, "http://"), strings.HasPrefix(srcURL, "https://"):
		fs, err = initHTTPFs(ctx, srcURL)
		if err != nil {
			return nil, errors.Wrap(ErrInitHTTPDownloader, err.Error())
		}
	default:
		return nil, errors.Wrap(ErrSourceURL, srcURL)
	}

	return fs, err
}

// DstURL returns the destination URL configured when initializing the downloader
func (c *Downloader) DstURL() string {
	return c.dstURL
}

// SrcURL returns the destination URL configured when initializing the downloader
func (c *Downloader) SrcURL() string {
	return c.srcURL
}

// Stats returns bytes, file transfer stats on the downloader
func (c *Downloader) Stats() *DownloaderStats {
	return &DownloaderStats{
		BytesTransferred:   rcloneStats.GlobalStats().GetBytes(),
		ObjectsTransferred: rcloneStats.GlobalStats().GetTransfers(),
		Errors:             rcloneStats.GlobalStats().GetErrors(),
	}
}

// SrcName returns the name of the source fs - set in the init*Fs methods
func (c *Downloader) SrcName() string {
	return c.src.Name()
}

// CopyFile copies src firmware in the c.src fs to c.dst fs
func (c *Downloader) CopyFile(ctx context.Context, fw *config.Firmware) error {
	var err error

	// In case the file already exists in dst, don't verify/copy it
	if exists, _ := rcloneFs.FileExists(ctx, c.dst, c.DstPath(fw)); exists {
		c.logger.WithFields(
			logrus.Fields{
				"filename": fw.Filename,
			},
		).Debug("firmware already exists at dst")

		return nil
	}

	err = c.VerifyFile(ctx, fw)
	if err != nil {
		return err
	}

	_, err = rcloneOperations.CopyURL(ctx, c.dst, c.DstPath(fw), c.srcURL, false, false, false)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+c.srcURL)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

func (c *Downloader) VerifyFile(ctx context.Context, fw *config.Firmware) error {
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(c.tmp.Root(), "verify-")
	if err != nil {
		return errors.Wrap(ErrCreatingTmpDir, err.Error())
	}

	defer os.RemoveAll(tmpDir)

	dstPath := path.Join(path.Base(tmpDir), fw.Filename)

	_, err = rcloneOperations.CopyURL(ctx, c.tmp, dstPath, c.srcURL, false, false, false)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+fw.Filename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	tmpFilename := path.Join(c.tmp.Root(), dstPath)

	c.logger.WithFields(
		logrus.Fields{
			"filename": tmpFilename,
			"checksum": fw.FileCheckSum,
		},
	).Debug("validating file")

	return SHA256ChecksumValidate(tmpFilename, fw.FileCheckSum)
}

func (c *Downloader) DstBucket() string {
	return c.dstCfg.Bucket
}

func (c *Downloader) SrcPath(fw *config.Firmware) string {
	u, _ := url.Parse(fw.UpstreamURL)
	return u.Path
}

func (c *Downloader) DstPath(fw *config.Firmware) string {
	return path.Join(
		"/firmware",
		UpdateFilesPath(
			c.vendor, fw.Model, fw.ComponentSlug, fw.Filename))
}

// initHttpFs initializes and returns a rcloneFs.Fs interface that can be used for Copy, Sync operations
// the Fs is initialized based the urlHost, urlPath parameters
//
// httpURL: the http endpoint which is expected to be the root/top level directory from where files are to be copied from/to
//
//	this can be a http index or a URL endpoint from which files are to be downloaded.
func initHTTPFs(ctx context.Context, httpURL string) (rcloneFs.Fs, error) {
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
func initLocalFs(ctx context.Context, cfg *LocalFsConfig) (rcloneFs.Fs, error) {
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
func initS3Fs(ctx context.Context, cfg *config.S3Bucket, root string) (rcloneFs.Fs, error) {
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

// UpdateFilesPath returns the directory, file path destination for the update
// based on the device vendor, model, component slug attributes
//
// This filepath structure is used to store and retrieve firmware
func UpdateFilesPath(deviceVendor, deviceModel, slug, filename string) string {
	var p string
	// Update configuration for dells where a filename isn't specified indicates the updates are an entire repository
	if deviceVendor == common.VendorDell && filename == "" {
		p = "/" + deviceVendor + "/"
		return p
	}

	p = path.Join(
		"/",
		deviceVendor,
		deviceModel,
		slug,
		filename,
	)

	return strings.Replace(p, "//", "/", -1)
}
