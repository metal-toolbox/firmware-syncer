package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/stretchr/testify/assert"

	"github.com/sirupsen/logrus"
)

func Test_NewDownloader(t *testing.T) {
	cfg := &config.S3Bucket{
		SecretKey: "foo",
		AccessKey: "bar",
		Endpoint:  "endpoint",
		Bucket:    "stuff",
		Region:    "region",
	}
	cases := []struct {
		srcURL     string
		cfg        *config.S3Bucket
		wantSrcURL string
		wantDstURL string
		wantTmp    string
		err        error
		name       string
	}{
		{
			"https://foo/bar/baz.bin",
			cfg,
			"https://foo/bar/baz.bin",
			"",
			"/tmp",
			nil,
			"valid downloader object returned",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewDownloader(context.TODO(), tc.srcURL, tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			if tc.wantSrcURL != "" {
				assert.Equal(t, tc.wantSrcURL, got.srcURL)
			}

			if tc.wantSrcURL != "" {
				assert.Equal(t, tc.wantDstURL, got.dstURL)
			}

			if tc.wantTmp != "" {
				assert.Equal(t, tc.wantTmp, got.tmp.Root())
			}

			if tc.cfg != nil {
				assert.Equal(t, tc.cfg, got.dstCfg)
			}
		})
	}
}

func Test_S3Downloader(t *testing.T) {
	vendor := "asrockrack"
	logLevel := logrus.InfoLevel
	srcConfig := &config.S3Bucket{
		Region:    "region",
		SecretKey: "foo",
		AccessKey: "bar",
		Endpoint:  "endpoint",
		Bucket:    "src-bucket",
	}
	dstConfig := &config.S3Bucket{
		Region:    "region",
		SecretKey: "foo",
		AccessKey: "bar",
		Endpoint:  "endpoint",
		Bucket:    "dst-bucket",
	}

	cases := []struct {
		name          string
		srcCfg        *config.S3Bucket
		dstCfg        *config.S3Bucket
		wantSrcBucket string
		wantDstBucket string
		err           error
	}{
		{
			"S3 downloader initialization",
			srcConfig,
			dstConfig,
			"src-bucket",
			"dst-bucket",
			nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewS3Downloader(context.TODO(), vendor, tc.srcCfg, tc.dstCfg, logLevel)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			if tc.wantSrcBucket != "" {
				assert.Equal(t, tc.wantSrcBucket, got.SrcBucket())
			}

			if tc.wantDstBucket != "" {
				assert.Equal(t, tc.wantDstBucket, got.DstBucket())
			}
		})
	}
}

func Test_initSource(t *testing.T) {
	cases := []struct {
		srcURL string
		err    error
		want   string
		name   string
	}{
		{
			"",
			ErrSourceURL,
			"",
			"sourceURL empty",
		},
		{
			"unsupported://foo/baz.bin",
			ErrSourceURL,
			"",
			"unsupported URL scheme",
		},
		{
			"http://foo/baz.bin",
			nil,
			"http://foo/baz.bin",
			"init source rclone fs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := initSource(context.TODO(), tc.srcURL)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.Nil(t, err)
			if tc.want != "" {
				assert.Equal(t, tc.want, f.Name())
			}
		})
	}
}

func Test_initLocalFs(t *testing.T) {
	cases := []struct {
		cfg  *LocalFsConfig
		err  error
		want string
		name string
	}{
		{
			nil,
			ErrFileStoreConfig,
			"",
			"fs config nil",
		},
		{
			&LocalFsConfig{},
			ErrRootDirUndefined,
			"",
			"root dir undefined",
		},
		{
			&LocalFsConfig{Root: "/foobar"},
			nil,
			"Local file system at /foobar",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := initLocalFs(context.TODO(), tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.Nil(t, err)
			if tc.want != "" {
				assert.Equal(t, tc.want, f.String())
			}
		})
	}
}

func Test_initS3Fs(t *testing.T) {
	cases := []struct {
		cfg  *config.S3Bucket
		root string
		err  error
		want string
		name string
	}{
		{
			nil,
			"",
			ErrFileStoreConfig,
			"",
			"s3 config nil",
		},
		{
			&config.S3Bucket{},
			"",
			ErrRootDirUndefined,
			"",
			"root dir undefined",
		},
		{
			&config.S3Bucket{},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Endpoint: "s3.example.foo"},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Endpoint: "s3.example.foo", AccessKey: "sekrit"},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Region: "region", Endpoint: "s3.example.foo", AccessKey: "sekrit", SecretKey: "sekrit"},
			"/foobar",
			nil,
			"S3 bucket foobar",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := initS3Fs(context.TODO(), tc.cfg, tc.root)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.Nil(t, err)
			if tc.want != "" {
				assert.Equal(t, tc.want, f.String())
			}
		})
	}
}

func Test_UpdateFilesPath(t *testing.T) {
	cases := []struct {
		hwvendor string
		hwmodel  string
		slug     string
		filename string
		expected string
	}{
		{
			"vendor",
			"model",
			"component",
			"foo.bin",
			"/vendor/model/component/foo.bin",
		},
		{
			"dell",
			"model",
			"",
			"",
			"/dell/",
		},
		{
			"dell",
			"model",
			"",
			"bios.bin",
			"/dell/model/bios.bin",
		},
	}

	for _, tt := range cases {
		p := UpdateFilesPath(tt.hwvendor, tt.hwmodel, tt.slug, tt.filename)
		assert.Equal(t, tt.expected, p)
	}
}

func Test_SplitURLPath(t *testing.T) {
	cases := []struct {
		httpURL  string
		hostPart string
		urlPart  string
		err      error
	}{
		{
			"https://example.com/foo/bar",
			"https://example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://example.com/foo/bar",
			"http://example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://example.com/foo/bar/",
			"http://example.com",
			"/foo/bar/",
			nil,
		},
		{
			"http://user:pass@example.com/foo/bar",
			"http://user:pass@example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://user:pass@example.com/foo/bar?foo=baz&lala=112",
			"http://user:pass@example.com",
			"/foo/bar?foo=baz&lala=112",
			nil,
		},
		{
			"file://example.com/foo/bar",
			"",
			"",
			ErrURLUnsupported,
		},
		{
			"https://example.com/foo/bar/foo.bin",
			"https://example.com",
			"/foo/bar/foo.bin",
			nil,
		},
		{
			"https://user:pass@example.com/foo/bar/foo.bin",
			"https://user:pass@example.com",
			"/foo/bar/foo.bin",
			nil,
		},
	}

	for _, tt := range cases {
		hostPart, urlPart, err := SplitURLPath(tt.httpURL)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
		} else {
			assert.Nil(t, err)
		}

		assert.Equal(t, tt.hostPart, hostPart)
		assert.Equal(t, tt.urlPart, urlPart)
	}
}
