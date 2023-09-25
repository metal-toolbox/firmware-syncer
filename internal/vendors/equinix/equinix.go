package equinix

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"

	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

const GithubDownloadTimeout = 300

// Equinix implements the Vendor interface methods to retrieve Equinix OpenBMC firmware files
type Equinix struct {
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	ghClient  *github.Client
	dstCfg    *config.S3Bucket
	dstFs     fs.Fs
	tmpFs     fs.Fs
}

func New(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger, v *viper.Viper) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.LoadEnvironmentVariable(v, logger, "github.openbmc_token")},
	)
	tc := oauth2.NewClient(ctx, ts)

	ghClient := github.NewClient(tc)

	// parse S3 endpoint and bucket from cfgSyncer.RepositoryURL
	s3DstEndpoint, s3DstBucket, err := config.ParseRepositoryURL(cfgSyncer.RepositoryURL)
	if err != nil {
		return nil, err
	}

	dstS3Config := &config.S3Bucket{
		Region:    cfgSyncer.RepositoryRegion,
		Endpoint:  s3DstEndpoint,
		Bucket:    s3DstBucket,
		AccessKey: config.LoadEnvironmentVariable(v, logger, "s3.access_key"),
		SecretKey: config.LoadEnvironmentVariable(v, logger, "s3.secret_key"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger, v)
	if err != nil {
		return nil, err
	}

	// init rclone filesystems for tmp and dst files
	vendors.SetRcloneLogging(logger)

	dstFs, err := vendors.InitS3Fs(ctx, dstS3Config, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	return &Equinix{
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		ghClient:  ghClient,
		dstCfg:    dstS3Config,
		dstFs:     dstFs,
		tmpFs:     tmpFs,
	}, nil
}

func (e *Equinix) Stats() *vendors.Metrics {
	return e.metrics
}

func (e *Equinix) Sync(ctx context.Context) error {
	for _, fw := range e.firmwares {
		// In case the file already exists in dst, don't copy it
		if exists, _ := fs.FileExists(ctx, e.dstFs, vendors.DstPath(fw)); exists {
			e.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		err := e.getFileFromGithub(ctx, fw)
		if err != nil {
			return err
		}

		// Verify file checksum
		tmpFilename := e.tmpFs.Root() + "/" + fw.Filename
		if !vendors.ValidateChecksum(tmpFilename, fw.Checksum) {
			return errors.Wrap(vendors.ErrChecksumValidate, fmt.Sprintf("tmpFilename: %s, expected checksum: %s", tmpFilename, fw.Checksum))
		}

		e.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": vendors.DstPath(fw),
			},
		).Info("sync Equinix")

		// Copy from tmpfs to dstfs
		err = operations.CopyFile(ctx, e.dstFs, e.tmpFs, vendors.DstPath(fw), fw.Filename)
		if err != nil {
			return err
		}

		err = e.inventory.Publish(ctx, fw, vendors.DstPath(fw))
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *Equinix) getFileFromGithub(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) error {
	owner, repo, tag, filename, err := parseGithubReleaseURL(fw.UpstreamURL)
	if err != nil {
		return err
	}

	release, _, err := e.ghClient.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return err
	}

	asset, err := getAssetByName(filename, release.Assets)
	if err != nil {
		return err
	}

	// Give enough time for the client to download the binary file.
	redirectClient := &http.Client{
		Timeout: time.Second * GithubDownloadTimeout,
	}

	rc, _, err := e.ghClient.Repositories.DownloadReleaseAsset(ctx, owner, repo, *asset.ID, redirectClient)
	if err != nil {
		return err
	}
	defer rc.Close()

	// Copy downloaded file to tmpFs for checksum verification and later upload to dst
	_, err = operations.Rcat(ctx, e.tmpFs, fw.Filename, rc, time.Now(), nil)
	if err != nil {
		return err
	}

	return nil
}

func parseGithubReleaseURL(ghURL string) (owner, repo, release, filename string, err error) {
	// https://github.com/<owner>/<repo>/releases/download/<tag>/<filename>
	u, err := url.Parse(ghURL)
	if err != nil {
		return "", "", "", "", err
	}

	components := strings.Split(u.Path, "/")
	if len(components) != 7 {
		return "", "", "", "", errors.New(fmt.Sprintf("parsing failed for URL path: %s", u.Path))
	}

	return components[1], components[2], components[5], components[6], nil
}

func getAssetByName(assetName string, assets []*github.ReleaseAsset) (asset *github.ReleaseAsset, err error) {
	for _, a := range assets {
		if assetName == *a.Name {
			return a, nil
		}
	}

	return nil, errors.New("asset doesn't exist with given name")
}
