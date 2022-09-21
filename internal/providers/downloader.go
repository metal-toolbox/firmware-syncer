package providers

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmc-toolbox/common"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/pkg/errors"

	rcloneHttp "github.com/rclone/rclone/backend/http"
	rcloneLocal "github.com/rclone/rclone/backend/local"
	rcloneS3 "github.com/rclone/rclone/backend/s3"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneStats "github.com/rclone/rclone/fs/accounting"
	rcloneConfigmap "github.com/rclone/rclone/fs/config/configmap"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
	rcloneSync "github.com/rclone/rclone/fs/sync"
)

const (
	KindLocal = "local"
	KindS3    = "s3"
	KindHTTP  = "http"
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
)

// Downloader wraps src and dst rclone Fs interface types to enable copying objects
type Downloader struct {
	// srcURL is the source URL configured for the src fs
	srcURL string
	// dstURL is the destination URL for the dst fs
	dstURL string
	// src is the remote file store
	src rcloneFs.Fs
	// filestore is the local/remote file store
	filestore rcloneFs.Fs
	// tmp is a temporary work file store
	tmp rcloneFs.Fs
	// StoreConfig this downloader was initialized with
	storeCfg *StoreConfig
}

// DownloaderStats includes fields for stats on file/object transfer for Downloader
type DownloaderStats struct {
	BytesTransferred   int64
	ObjectsTransferred int64
	Errors             int64
}

// StoreConfig holds attributes for the filestore where files are downloaded
type StoreConfig struct {
	// URL points to the destination file store, the filestore is initialized based on the url scheme
	// examples:
	//   s3://<bucket-name>/<root>
	//   local:///tmp/foo
	URL string
	// Path to mount as the tmp directory when downloading files to sign and verify
	Tmp string
	// S3 configuration - required when URL points to an s3 bucket
	S3 *config.S3Bucket
	// Local filesystem configuration - required when URL points to a local directory
	Local *LocalFsConfig
	// Path to root of the fs
	Root string
}

// LocalFsConfig for the downloader
type LocalFsConfig struct {
	Root string
}

// NewDownloader initializes a downloader object based on the srcURL and the given StoreConfig
func NewDownloader(ctx context.Context, srcURL string, storeCfg *StoreConfig) (*Downloader, error) {
	var err error

	downloader := &Downloader{srcURL: srcURL}

	downloader.filestore, err = initStore(ctx, storeCfg)
	if err != nil {
		return nil, err
	}

	// init local tmp fs to generate checksum and signature files
	downloader.tmp, err = initLocalFs(ctx, &LocalFsConfig{Root: storeCfg.Tmp})
	if err != nil {
		return nil, err
	}

	// init source to download files
	downloader.src, err = initSource(ctx, srcURL)
	if err != nil {
		return nil, err
	}

	downloader.storeCfg = storeCfg

	return downloader, nil
}

// FilestoreConfig accepts a srcURL and config.Filestore to return a StoreConfig
// that can be passed to init a downloader
//
// This method sets up the StoreConfig.URL based on the filestore configuration included
// nolint:gocyclo // validation is cyclomatic
func FilestoreConfig(rootDir string, cfg *config.Filestore) (*StoreConfig, error) {
	if cfg == nil || cfg.TmpDir == "" {
		return nil, errors.Wrap(ErrStoreConfig, "config nil or no TmpDir defined")
	}

	storeCfg := &StoreConfig{Tmp: cfg.TmpDir}

	switch cfg.Kind {
	case KindS3:
		if cfg.S3 == nil ||
			cfg.S3.Bucket == "" ||
			cfg.S3.SecretKey == "" ||
			cfg.S3.AccessKey == "" ||
			cfg.S3.Endpoint == "" ||
			cfg.S3.Region == "" {
			return nil, errors.Wrap(ErrStoreConfig, "s3 configuration nil or undefined")
		}

		storeCfg.S3 = cfg.S3
		storeCfg.Root = rootDir

		storeCfg.URL = cfg.S3.Endpoint + "/" + cfg.S3.Bucket + "/"

		// prefix s3:// scheme
		if !strings.HasPrefix(cfg.S3.Endpoint, "s3://") {
			storeCfg.URL = "s3://" + storeCfg.URL
		}

	case KindLocal:
		storeCfg.Local = &LocalFsConfig{Root: cfg.LocalDir}
		storeCfg.URL = cfg.LocalDir
		storeCfg.Root = rootDir

		if !strings.HasPrefix(storeCfg.URL, "local://") {
			storeCfg.URL = "local://" + storeCfg.URL
		}
	default:
		return nil, errors.Wrap(ErrStoreConfig, "unsupport filestore Kind: %s"+cfg.Kind)
	}

	return storeCfg, nil
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

// initStore initializes the file store based on StoreConfig and returns a rclone.Fs with Copy/Sync methods
func initStore(ctx context.Context, cfg *StoreConfig) (rcloneFs.Fs, error) {
	var err error

	var fs rcloneFs.Fs

	if cfg == nil {
		return nil, errors.Wrap(ErrFileStoreConfig, "got nil")
	}

	// init store configuration
	switch {
	case strings.HasPrefix(cfg.URL, "s3://"):
		fs, err = initS3Fs(ctx, cfg.S3, cfg.Root)
	case strings.HasPrefix(cfg.URL, "local://"):
		fs, err = initLocalFs(ctx, cfg.Local)
	default:
		return nil, errors.Wrap(ErrUnsupportedFileStore, cfg.URL)
	}

	return fs, err
}

// StoreURL the file store URL configured for the downloader
func (c *Downloader) FilestoreURL() string {
	return strings.TrimSuffix(c.storeCfg.URL, "/")
}

func (c *Downloader) FilestoreRootDir() string {
	if c.storeCfg.S3 != nil {
		return c.storeCfg.Root
	}

	if c.storeCfg.Local != nil {
		return c.storeCfg.Local.Root
	}

	return ""
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

// CopyFilestoreToLocalTmp copies files from the downloader.dst fs into the local tmp directory
func (c *Downloader) CopyFilestoreToLocalTmp(ctx context.Context, tmpFilename, srcFilename string) error {
	err := rcloneOperations.CopyFile(ctx, c.tmp, c.filestore, tmpFilename, srcFilename)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+": "+srcFilename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

// CopyLocalTmpToFilestore copies files from the local tmp directory to the downloader.dst fs
func (c *Downloader) CopyLocalTmpToFilestore(ctx context.Context, dstFilename, srcFilename string) error {
	err := rcloneOperations.CopyFile(ctx, c.filestore, c.tmp, dstFilename, srcFilename)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+": "+srcFilename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

// CopyURLToLocalTmp copies files from the srcURL to the local tmp directory
func (c *Downloader) CopyURLToLocalTmp(ctx context.Context, tmpFilename, srcURL string) error {
	_, err := rcloneOperations.CopyURL(ctx, c.tmp, tmpFilename, srcURL, false, false, false)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+": "+srcURL)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

// CopyFile copies srcFile frm the src fs to dstFile in the filestore fs
//
// srcFile: this is expected to be a relative path to the directory used as a mount point in the init*Fs methods
func (c *Downloader) CopyToFilestore(ctx context.Context, dstFilename, srcFilename string) error {
	err := rcloneOperations.CopyFile(ctx, c.filestore, c.src, dstFilename, srcFilename)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(ErrCopy, err.Error()+" :"+srcFilename)
		}

		return errors.Wrap(ErrCopy, err.Error())
	}

	return nil
}

// Sync syncronises files between the srcURL and the dst filestore
func (c *Downloader) Sync(ctx context.Context) error {
	err := rcloneSync.Sync(ctx, c.filestore, c.src, true)
	if err != nil {
		return errors.Wrap(ErrSync, err.Error())
	}

	return nil
}

// DstFileExists checks if the given file exists in the destination
//
// note: the name parameter must be the full path name to the file including the file name
//
//	e.g: "/foo/bar/lala.bin"
func (c *Downloader) FilestoreFileExists(ctx context.Context, name string) (bool, error) {
	exists, err := fileExists(ctx, c.filestore, name)
	if err != nil {
		return false, errors.Wrap(ErrCheckFileExists, err.Error())
	}

	return exists, nil
}

// SrcFileExists checks if the given file exists in the source
//
// note: the name parameter must be the full path name to the file including the file name
//
//	e.g: "/foo/bar/lala.bin"
func (c *Downloader) SrcFileExists(ctx context.Context, name string) (bool, error) {
	exists, err := fileExists(ctx, c.src, name)
	if err != nil {
		return false, errors.Wrap(ErrCheckFileExists, err.Error())
	}

	return exists, nil
}

// fileExists returns true if a file exists, returns false if its a directory
func fileExists(ctx context.Context, fs rcloneFs.Fs, name string) (bool, error) {
	return rcloneFs.FileExists(ctx, fs, name)
}

// FilestoreFileModTime returns the file modification time in the destination
//
// Note: mod time is not available for s3 directory objects
func (c *Downloader) FilestoreFileModTime(ctx context.Context, name string) (time.Time, error) {
	var modtime time.Time

	exists, err := fileExists(ctx, c.filestore, name)
	if err != nil {
		return modtime, errors.Wrap(ErrCheckFileExists, err.Error())
	}

	if !exists {
		return modtime, errors.Wrap(ErrFileNotFound, name)
	}

	object, err := FindFileObjectByName(ctx, c.filestore, name)
	if err != nil {
		return modtime, errors.Wrap(ErrModTimeFile, err.Error())
	}

	return object.ModTime(ctx), nil
}

// FindFileObjectByName searches the directory of the given name and returns the matched file object
// returns nil if the object was not found
func FindFileObjectByName(ctx context.Context, fs rcloneFs.Fs, name string) (rcloneFs.Object, error) {
	path := filepath.Dir(name)

	objectsS3, err := fs.List(ctx, path)
	if err != nil {
		return nil, errors.Wrap(ErrListingFiles, err.Error())
	}

	if objectsS3.Len() == 0 {
		return nil, nil
	}

	var object rcloneFs.Object

	objectsS3.ForObject(func(obj rcloneFs.Object) {
		// Trim prefix, since obj.String has no / prefix
		if obj.String() == strings.TrimPrefix(name, "/") && !strings.HasSuffix(obj.String(), "/") {
			object = obj
		}
	})

	if object == nil {
		return nil, errors.Wrap(ErrFileNotFound, name)
	}

	return object, nil
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

	p = "/" + strings.Join([]string{
		deviceVendor,
		deviceModel,
		slug,
		filename,
	}, "/")

	return strings.Replace(p, "//", "/", -1)
}
