package dell

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/equinixmetal/firmware-syncer/internal/config"
	"github.com/equinixmetal/firmware-syncer/internal/providers"
	"github.com/stretchr/testify/assert"
)

func Test_initDownloaderDUP(t *testing.T) {
	fsConfig := &config.Filestore{
		Kind:   "s3",
		TmpDir: "/tmp",
		S3: &config.S3Bucket{
			SecretKey: "foo",
			AccessKey: "bar",
			Endpoint:  "endpoint",
			Bucket:    "stuff",
		},
	}

	cases := []struct {
		srcURL     string
		cfg        *config.Filestore
		wantSrcURL string
		wantDstURL string
		err        error
		name       string
	}{
		{
			"https://foo/bar/baz.bin",
			fsConfig,
			"https://foo/bar",
			"",
			nil,
			"DUP downloader initialized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := initDownloaderDUP(context.TODO(), tc.srcURL, tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			if tc.wantSrcURL != "" {
				assert.Equal(t, tc.wantSrcURL, got.SrcURL())
			}

			if tc.wantSrcURL != "" {
				assert.Equal(t, tc.wantDstURL, got.DstURL())
			}
		})
	}
}

func Test_initDownloaderDSU(t *testing.T) {
	dsuUpdateCfg := &model.UpdateConfig{
		UpstreamURL: "http://foo/bar/DSU_1.2.3",
		Utility:     "dsu",
	}

	filestoreCfg := &config.Filestore{
		Kind:   "s3",
		TmpDir: "/tmp",
		S3: &config.S3Bucket{
			SecretKey: "foo",
			AccessKey: "bar",
			Endpoint:  "endpoint",
			Bucket:    "stuff",
		},
	}

	repoPathz, err := repoPaths(dsuUpdateCfg, repoFilesUnset)
	if err != nil {
		t.Error(err)
	}

	cases := []struct {
		syncCtx     *providers.SyncerContext
		wantCount   int
		wantSrcURLs []string
		wantDstURLs []string
		err         error
		name        string
	}{
		{
			&providers.SyncerContext{
				UpdateDirPrefix: "/firmware",
				HWVendor:        "dell",
				HWModel:         "r640",
				UpdateCfg:       dsuUpdateCfg,
				FilestoreCfg:    filestoreCfg,
			},
			2,
			[]string{
				"http://foo/bar/DSU_1.2.3/os_dependent/RHEL8_64",
				"http://foo/bar/DSU_1.2.3/os_independent",
			},
			[]string{},
			nil,
			"DSU downloader initialized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := initDownloadersDSU(context.TODO(), repoPathz, tc.syncCtx)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			assert.Equal(t, tc.wantCount, len(got))
			if tc.wantCount > 0 {
				for _, d := range got {
					fmt.Println(d.SrcURL())
					if !utils.StringInSlice(d.SrcURL(), tc.wantSrcURLs) {
						t.Error("expected downloader with srcURL")
						continue
					}
				}
			}
		})
	}
}

func Test_dstUpdateDir(t *testing.T) {
	cases := []struct {
		repoFileDir string
		syncCtx     *providers.SyncerContext
		expected    string
		err         error
	}{
		{
			"",
			&providers.SyncerContext{HWVendor: "dell", HWModel: "all"},
			"",
			ErrDstUpdateDir,
		},
		{
			"",
			&providers.SyncerContext{UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "all", UpdateCfg: &model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3", Filename: "foobar.bin"}},
			"",
			ErrDstUpdateDir,
		},
		{
			"/DSU_1.2.3/os_dependent/RHEL8_64/repodata/primary.xml.gz",
			&providers.SyncerContext{UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "all", UpdateCfg: &model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3", Utility: "dsu"}},
			"/firmware/dell/DSU_1.2.3/os_dependent/RHEL8_64",
			nil,
		},
		{
			"/DSU_1.2.3/os_independent/repodata/primary.xml.gz",
			&providers.SyncerContext{UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "all", UpdateCfg: &model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3", Utility: "dsu"}},
			"/firmware/dell/DSU_1.2.3/os_independent",
			nil,
		},
		{
			"",
			&providers.SyncerContext{UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "r640", ComponentSlug: "bios", UpdateCfg: &model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3", Filename: "foobar.bin", Utility: "dup"}},
			"/firmware/dell/r640/bios/foobar.bin",
			nil,
		},
	}

	for _, tt := range cases {
		d, err := dstUpdateDir(tt.repoFileDir, tt.syncCtx)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
		} else {
			assert.Nil(t, err)
		}

		assert.Equal(t, tt.expected, d)
	}
}

func Test_dsuDir(t *testing.T) {
	cases := []struct {
		upstreamURL string
		expected    string
		err         error
	}{
		{
			"http://foo/bar/DSU_1.2.3",
			"DSU_1.2.3",
			nil,
		},
		{
			"http://foo/bar/INVALID/",
			"",
			ErrDellUpstreamURL,
		},
	}

	for _, tt := range cases {
		d, err := dsuDir(tt.upstreamURL)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
		} else {
			assert.Nil(t, err)
		}

		assert.Equal(t, tt.expected, d)
	}
}

func Test_repoPaths(t *testing.T) {
	cases := []struct {
		updateCfg *model.UpdateConfig
		repoFiles map[string]string
		expected  map[string]string
		err       error
	}{
		{
			&model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3/"},
			repoFilesUnset,
			map[string]string{
				"os_dependent":   "/DSU_1.2.3/os_dependent/RHEL8_64/repodata/primary.xml.gz",
				"os_independent": "/DSU_1.2.3/os_independent/repodata/primary.xml.gz",
			},
			nil,
		},
		{
			&model.UpdateConfig{UpstreamURL: "http://foo/bar/DSU_1.2.3"},
			repoFilesUnset,
			map[string]string{
				"os_dependent":   "/DSU_1.2.3/os_dependent/RHEL8_64/repodata/primary.xml.gz",
				"os_independent": "/DSU_1.2.3/os_independent/repodata/primary.xml.gz",
			},
			nil,
		},
		{
			&model.UpdateConfig{UpstreamURL: "http://foo/bar/invalid"},
			repoFilesUnset,
			nil,
			ErrDellUpstreamURL,
		},
		{
			&model.UpdateConfig{
				UpstreamURL: "http://foo/bar/DSU_1.2.3",
				Meta:        map[string]string{"os": "invalidos"},
			},
			repoFilesUnset,
			nil,
			ErrDellOSRelease,
		},
	}

	for _, tt := range cases {
		m, err := repoPaths(tt.updateCfg, tt.repoFiles)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
		} else {
			assert.Nil(t, err)
		}

		assert.Equal(t, tt.expected, m)
	}
}

func Test_dsuUpdateURL(t *testing.T) {
	testCases := []struct {
		syncCtx  *providers.SyncerContext
		expected string
		err      error
		testName string
	}{
		{
			&providers.SyncerContext{HWVendor: "dell", HWModel: "all"},
			"",
			providers.ErrSyncerContextAttributes,
			"UpdateStoreURL unset",
		},
		{
			&providers.SyncerContext{
				UpdateCfg:      &model.UpdateConfig{UpstreamURL: "https://linux.foo.bar/bar/DSU_1.2.3"},
				UpdateStoreURL: "https://foo.baz", UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "all",
			},
			"https://foo.baz/firmware/dell/DSU_1.2.3",
			nil,
			"DSU filestore update URL returned",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			got, err := dsuUpdateURL(tc.syncCtx)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.expected, got)
			}
		})
	}
}

func Test_dupUpdateURL(t *testing.T) {
	testCases := []struct {
		syncCtx  *providers.SyncerContext
		expected string
		err      error
		testName string
	}{
		{
			&providers.SyncerContext{HWVendor: "dell", HWModel: "r7600"},
			"",
			providers.ErrSyncerContextAttributes,
			"UpdateStoreURL unset",
		},
		{
			&providers.SyncerContext{
				UpdateCfg:      &model.UpdateConfig{UpstreamURL: "https://linux.foo.bar/bar/DUP.bin", Filename: "DUP.bin"},
				ComponentSlug:  "bios",
				UpdateStoreURL: "https://foo.baz", UpdateDirPrefix: "/firmware", HWVendor: "dell", HWModel: "r7600",
			},
			"https://foo.baz/firmware/dell/r7600/bios/DUP.bin",
			nil,
			"DUP filestore update URL returned",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			got, err := dupUpdateURL(tc.syncCtx)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.expected, got)
			}
		})
	}
}
