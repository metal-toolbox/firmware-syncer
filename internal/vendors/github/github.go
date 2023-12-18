package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

	"github.com/metal-toolbox/firmware-syncer/internal/vendors"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

const DownloadTimeout = 300

func NewGitHubClient(ctx context.Context, githubOpenBmcToken string) *github.Client {
	tokenSource := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubOpenBmcToken},
	)
	tokenClient := oauth2.NewClient(ctx, tokenSource)

	return github.NewClient(tokenClient)
}

type Downloader struct {
	logger *logrus.Logger
	client *github.Client
}

func NewGitHubDownloader(logger *logrus.Logger, client *github.Client) vendors.Downloader {
	return &Downloader{
		logger: logger,
		client: client,
	}
}

func (d *Downloader) Download(
	ctx context.Context,
	downloadDir string,
	firmware *serverservice.ComponentFirmwareVersion,
) (string, error) {
	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: downloadDir})
	if err != nil {
		return "", err
	}

	owner, repo, tag, filename, err := parseGithubReleaseURL(firmware.UpstreamURL)
	if err != nil {
		return "", err
	}

	release, _, err := d.client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return "", err
	}

	asset, err := getAssetByName(filename, release.Assets)
	if err != nil {
		return "", err
	}

	// Give enough time for the client to download the binary file.
	redirectClient := &http.Client{
		Timeout: time.Second * DownloadTimeout,
	}

	rc, _, err := d.client.Repositories.DownloadReleaseAsset(ctx, owner, repo, *asset.ID, redirectClient)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	_, err = operations.Rcat(ctx, tmpFs, firmware.Filename, rc, time.Now(), nil)
	if err != nil {
		return "", err
	}

	return path.Join(downloadDir, firmware.Filename), nil
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
