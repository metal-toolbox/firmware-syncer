package dell

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/equinixmetal/firmware-syncer/internal/config"
	"github.com/equinixmetal/firmware-syncer/internal/providers"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilDellDSU = "dsu" // Dell System Update utility
	UpdateUtilDellDUP = "dup" // Dell Update Package
)

// This provider syncs updates from the Dell update endpoints
//
// Based on the update utility (UpdateConfig.Utility) - which is one of DSU or DUP
// the dell provider retrieves the updates into the remote/local filestore
//
// DSU repositories contain a metadata file - the primary.xml.gz file, which includes the SHA checksums for each RPM package,
// this file is checksum'd and signed as primary.xml.gz.sha256, primary.xml.gz.sha256.sig
//
// DUP files are retrieved into the filestore, checksummed and signed by themselves.
//
//
// Its often the case that the upstream is unavailable or has incorrect repodata
// and hence the sync is based on the checksum and sig files being present,
// instead of attempting to compare all the files each time.
//
//
// Updates files end up in the configured filestore under a directory structure determined by the
// hardware vendor, model, component slug and update filename (if any)

var (
	osRelease = map[string]string{
		"rhel8":   "RHEL8_64",
		"default": "RHEL8_64",
	}

	// repoFilesUnset is an 'unset' map of dell repository names to the repofile path
	// the OS release and the DSU version are 'set' at provider init
	repoFilesUnset = map[string]string{
		// /$DSU version/os_dependent/$OS release/
		"os_dependent":   "/%s/os_dependent/%s/repodata/primary.xml.gz",
		"os_independent": "/%s/os_independent/repodata/primary.xml.gz",
	}

	ErrDellUpstreamURL  = errors.New("expected upstreamURL with a /DSU_ url fragment suffixed")
	ErrDellRepofilePath = errors.New("expected path containing /repodata/primary.xml.gz")
	ErrDellRepoDir      = errors.New("unsupported repo directory")
	ErrDellOSRelease    = errors.New("unsupported OS release defined in meta[os]")
	ErrUploadSigned     = errors.New("error uploading signed file(s)")
	ErrDownloadSign     = errors.New("error downloading files to sign")
	ErrDownloadVerify   = errors.New("error downloading files to verify")
	ErrNotInSync        = errors.New("files not in sync")
	ErrRemoteSignFail   = errors.New("error in generating checksum and signature for remote repofiles")
	ErrRemoteVerifyFail = errors.New("error in verifying signature for remote repofiles")
	ErrDstUpdateDir     = errors.New("error identifying destination update directory path")
	ErrUpdateUtil       = errors.New("unsupported update utility")
)

// Dell implements the Provider interface methods to retrieve dell updates
type Dell struct {
	updateUtility string
	config        *config.Provider
	firmwares     []*config.Firmware

	logger *logrus.Logger
}

// New returns a new Dell firmware syncer object
func New(ctx context.Context, cfgProvider *config.Provider, logger *logrus.Logger) (providers.Provider, error) {
	// UpstreamURL required
	if cfgProvider.UpstreamURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined")
	}

	// RepositoryURL required
	if cfgProvider.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	// Dell update utility must be one of DSU, DUP
	if stringInSlice(cfgProvider.Utility, []string{UpdateUtilDellDSU, UpdateUtilDellDUP}) {
		return nil, errors.Wrap(config.ErrProviderAttributes, "unknown update utility: "+cfgProvider.Utility)
	}

	// Dell DUP files should be passed in with the file sha256 checksum
	if cfgProvider.Utility == UpdateUtilDellDUP && cfgProvider.UtilityChecksum == "" {
		return nil, errors.Wrap(config.ErrNoFileChecksum, cfgProvider.UpstreamURL)
	}

	return &Dell{
		updateUtility: cfgProvider.Utility,
		config:        cfgProvider,
		firmwares:     cfgProvider.Firmwares,
		logger:        logger,
	}, nil
}

// Stats implements the Syncer interface to return metrics collected on Object, byte transfer stats
//func (d *Dell) Stats() *providers.Metrics {
//	return d.Metrics
//}

// initDownloaderDUP initializes the dell DUP file downloader
// downloaders provides methods to Copy and Sync update file(s) from the source to the destination
//
// Dell updates installed as DUPs (updateConfig.Utility)
// these have a single downloader to retrieve the file from the UpstreamURL
func initDownloaderDUP(ctx context.Context, srcURL string, filestoreCfg *config.Filestore) (*providers.Downloader, error) {
	// Split out host and url part so the downloader can be invoked to copy with the source filename
	hostPart, urlPath, err := providers.SplitURLPath(srcURL)
	if err != nil {
		return nil, err
	}

	// upstream URL parent
	urlPathDir := filepath.Dir(urlPath)
	// srcURLPart includes the scheme + host + url path of the srcURL (excluding the base name)
	srcURLPart := hostPart + urlPathDir

	storeCfg, err := providers.FilestoreConfig("/", filestoreCfg)
	if err != nil {
		return nil, err
	}

	return providers.NewDownloader(ctx, srcURLPart, storeCfg)
}

// initDownloaderDSU initializes a dell DSU downloader
// downloaders provides methods to Copy and Sync update file(s) from the source to the destination
//
// Dell updates installed with the DSU update utility (updateConfig.Utility)
// have two UpstreamURL endpoints to fetch updates from, hence this is a map
// with the os_dependent, os_independent endpoint downloaders,
// both downloaders have the filestore configured as the dst
//
// Note: the dst root directory is /firmware/dell/DSU_<version>/{os_independent, os_dependent}
//       any dst sync should be relative to these paths
func initDownloadersDSU(ctx context.Context, cfgProvider *config.Provider) (map[string]*providers.Downloader, error) {
	var err error
	// when the utility is DSU, init repo file paths
	// declare firmware dest directory path
	// init repo file paths
	repoFiles := make(map[string]string)
	for _, fw := range cfgProvider.Firmwares {
		if fw.Utility == UpdateUtilDellDSU {
			repoFiles, err = repoPaths(fw, repoFilesUnset)
			if err != nil {
				return nil, err
			}
		}

	}

	// returned downloaders
	downloaders := make(map[string]*providers.Downloader)

	// update utility is DSU
	for repoFileDir, repoFilePath := range repoFiles {
		// /firmware/dell/DSU_<version>/{os_independent, os_dependent}
		downloadPath, err := dstUpdateDir(repoFilePath, cfgFirmware)
		if err != nil {
			return nil, err
		}

		srcURL, err := srcRepoURL(syncCtx.UpdateCfg, repoFileDir)
		if err != nil {
			return nil, err
		}

		// init the downloader file store config
		storeCfg, err := providers.FilestoreConfig(downloadPath, syncCtx.FilestoreCfg)
		if err != nil {
			return nil, err
		}

		// init downloader
		downloader, err := providers.NewDownloader(ctx, srcURL, storeCfg)
		if err != nil {
			return nil, err
		}

		downloaders[repoFileDir] = downloader
	}

	return downloaders, nil
}

// srcRepoURL returns the source repository URL based on the repoFileDir and the update configuration
func srcRepoURL(updateCfg *model.UpdateConfig, repoFileDir string) (string, error) {
	if !strings.HasSuffix(updateCfg.UpstreamURL, "/") {
		updateCfg.UpstreamURL += "/"
	}

	switch repoFileDir {
	case "os_independent":
		return updateCfg.UpstreamURL + "os_independent", nil
	case "os_dependent":
		var cfgOS string

		cfgOS, err := releaseOS(updateCfg)
		if err != nil {
			return "", err
		}

		return updateCfg.UpstreamURL + "os_dependent/" + cfgOS, nil
	default:
		return "", errors.Wrap(ErrDellRepoDir, repoFileDir)
	}
}

// dstUpdateDir returns the directory where the updates should be downloaded into
// based on the repoFilePath, and the syncCtx, where,
//
// repoFilePath is the repo file path including the filename or empty if update utility is dup
// syncCtx includes hardware attributes like the vendor, model, component name, update file name
func dstUpdateDir(repoFilePath string, cfgFirmware *config.Firmware) (string, error) {
	if syncCtx.UpdateCfg == nil {
		syncCtx.UpdateCfg = &model.UpdateConfig{}
	}
	// The update file path is determined from the hardware attributes
	updateDir := UpdateFilesPath(
		cfgFirmware.Vendor,
		cfgFirmware.Model,
		cfgFirmware.ComponentSlug,
		cfgFirmware.Filename,
	)

	if cfgFirmware.Utility == UpdateUtilDellDSU {
		//	"/DSU_1.2.3/os_dependent/RHEL8_64/repodata/primary.xml.gz",
		idx := strings.Index(repoFilePath, "/repodata/primary.xml.gz")
		if idx <= 0 {
			return "", errors.Wrap(ErrDellRepofilePath, "got: "+repoFilePath)
		}

		repoFileDir := repoFilePath[:idx]

		return filepath.Join(cfgFirmware.UpdateDirPrefix, updateDir, repoFileDir), nil
	}

	if cfgFirmware.Utility == UpdateUtilDellDUP {
		return filepath.Join(cfgFirmware.UpdateDirPrefix, updateDir), nil
	}

	return "", errors.Wrap(ErrDstUpdateDir, "unknown update utility: "+cfgFirmware.Utility)
}

// dsuDir returns the DSU_<version> base directory in the upstreamURL
func dsuDir(upstreamURL string) (string, error) {
	basePath := filepath.Base(upstreamURL)
	if !strings.HasPrefix(basePath, "DSU_") {
		return "", errors.Wrap(ErrDellUpstreamURL, upstreamURL)
	}

	return basePath, nil
}

func releaseOS(cfg *config.Firmware) (string, error) {
	var cfgOS string
	// set release OS path
	if len(cfg.Meta) > 0 && cfg.Meta["os"] != "" {
		var exists bool

		cfgOS, exists = osRelease[cfg.Meta["os"]]
		if !exists {
			return "", errors.Wrap(ErrDellOSRelease, cfg.Meta["os"])
		}
	} else {
		cfgOS = osRelease["default"]
	}

	return cfgOS, nil
}

// repoPaths fills in the DSU and OS version based on the upstreamURL and returns the given repoPaths
func repoPaths(cfg *config.Firmware, repoPaths map[string]string) (map[string]string, error) {
	// DSU_<version>
	basePath, err := dsuDir(cfg.UpstreamURL)
	if err != nil {
		return nil, err
	}

	m := make(map[string]string)

	for k, p := range repoPaths {
		if strings.Contains(k, "os_dependent") {
			m[k] = fmt.Sprintf(p, basePath, osRelease["default"])
		}

		if strings.Contains(k, "os_independent") {
			m[k] = fmt.Sprintf(p, basePath)
		}
	}

	return m, nil
}

// dsuUpdateURL returns the filestore UpdateURL for the DSU repository
//
// e.g: https://local-filestore/firmware/dell/DSU_V.V.V
func dsuUpdateURL(syncCtx *providers.SyncerContext) (string, error) {
	if syncCtx.UpdateStoreURL == "" {
		return "", errors.Wrap(providers.ErrSyncerContextAttributes, "UpdateStoreURL")
	}

	dsuVersion, err := dsuDir(syncCtx.UpdateCfg.UpstreamURL)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(syncCtx.UpdateStoreURL)
	if err != nil {
		return "", err
	}

	urlParts := []string{
		u.Path,
		syncCtx.UpdateDirPrefix,
		syncCtx.HWVendor,
		dsuVersion,
	}

	u.Path = path.Join(urlParts...)

	return u.String(), nil
}

// dupUpdateURL returns the filestore UpdateURL for the DUP update
//
// e.g: https://local-filestore/firmware/dell/r6000/foo.bin
func dupUpdateURL(syncCtx *providers.SyncerContext) (string, error) {
	if syncCtx.UpdateStoreURL == "" {
		return "", errors.Wrap(providers.ErrSyncerContextAttributes, "UpdateStoreURL")
	}

	u, err := url.Parse(syncCtx.UpdateStoreURL)
	if err != nil {
		return "", err
	}

	urlParts := []string{
		u.Path,
		syncCtx.UpdateDirPrefix,
		UpdateFilesPath(
			syncCtx.HWVendor,
			syncCtx.HWModel,
			syncCtx.ComponentSlug,
			syncCtx.UpdateCfg.Filename,
		),
	}

	u.Path = path.Join(urlParts...)

	return u.String(), nil
}

// stringInSlice returns true if the slice contains given string
// Can be removed once we update to go 1.18 where we can use slices.Contains()
func stringInSlice(str string, sl []string) bool {
	for _, element := range sl {
		if element == str {
			return true
		}
	}

	return false
}

// UpdateFilesPath returns the directory, file path destination for the update
// based on the device vendor, model, component slug attributes
//
// This filepath structure is used to store and retrieve firmware
func UpdateFilesPath(deviceVendor, deviceModel, slug, filename string) string {
	var p string
	// Update configuration for dells where a filename isn't specified indicates the updates are an entire repository
	if deviceVendor == ironlibm.VendorDell && filename == "" {
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
