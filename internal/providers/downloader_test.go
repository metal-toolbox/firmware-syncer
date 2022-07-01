package providers

import (
	"context"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/stretchr/testify/assert"
)

func Test_NewDownloader(t *testing.T) {
	sConfig := &StoreConfig{
		URL: "s3://endpoint/stuff/",
		Tmp: "/tmp",
		S3: &S3Config{
			SecretKey: "foo",
			AccessKey: "bar",
			Endpoint:  "endpoint",
			Bucket:    "stuff",
			Root:      "/test",
		},
	}
	cases := []struct {
		srcURL     string
		cfg        *StoreConfig
		wantSrcURL string
		wantDstURL string
		wantTmp    string
		err        error
		name       string
	}{
		{
			"https://foo/bar/baz.bin",
			sConfig,
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
				assert.Equal(t, tc.cfg, got.storeCfg)
			}
		})
	}
}

func Test_FilestoreConfig(t *testing.T) {
	cases := []struct {
		rootDir string
		cfg     *config.Filestore
		want    *StoreConfig
		err     error
		name    string
	}{
		{
			"/test",
			&config.Filestore{Kind: "invalid"},
			nil,
			ErrStoreConfig,
			"invalid filestore kind error returned",
		},
		{
			"/test",
			&config.Filestore{Kind: "s3"},
			nil,
			ErrStoreConfig,
			"invalid s3 configuration",
		},
		{
			"/test",
			&config.Filestore{Kind: "s3", S3: &config.S3Bucket{}},
			nil,
			ErrStoreConfig,
			"invalid s3 configuration",
		},
		{
			"/test",
			&config.Filestore{
				Kind:   "s3",
				TmpDir: "/tmp",
				S3: &config.S3Bucket{
					SecretKey: "foo",
					AccessKey: "bar",
					Endpoint:  "endpoint",
					Bucket:    "stuff",
				},
			},
			&StoreConfig{
				URL: "s3://endpoint/stuff/",
				Tmp: "/tmp",
				S3: &S3Config{
					SecretKey: "foo",
					AccessKey: "bar",
					Endpoint:  "endpoint",
					Bucket:    "stuff",
					Root:      "/test",
				},
			},
			nil,
			"valid s3 configuration",
		},
		{
			"/test",
			&config.Filestore{
				Kind:     "local",
				TmpDir:   "/tmp",
				LocalDir: "/foo/baz",
			},
			&StoreConfig{
				URL: "local:///foo/baz",
				Local: &LocalFsConfig{
					Root: "/foo/baz",
				},
				Tmp: "/tmp",
			},
			nil,
			"valid s3 configuration",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FilestoreConfig(tc.rootDir, tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			if tc.want != nil {
				assert.Equal(t, tc.want, got)
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

func Test_initStore(t *testing.T) {
	cases := []struct {
		cfg  *StoreConfig
		err  error
		want string
		name string
	}{
		{
			nil,
			ErrFileStoreConfig,
			"",
			"FileStore config nil",
		},
		{
			&StoreConfig{URL: "s3://bucket/foobar", S3: &S3Config{Root: "/foobar", Endpoint: "s3.example.foo", AccessKey: "sekrit", SecretKey: "sekrit"}},
			nil,
			"S3 bucket foobar",
			"init s3 filestore",
		},
		{
			&StoreConfig{URL: "local://tmp/updates", Local: &LocalFsConfig{Root: "/tmp/updates"}},
			nil,
			"Local file system at /tmp/updates",
			"init local fs filestore",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := initStore(context.TODO(), tc.cfg)
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
		cfg  *S3Config
		err  error
		want string
		name string
	}{
		{
			nil,
			ErrFileStoreConfig,
			"",
			"s3 config nil",
		},
		{
			&S3Config{},
			ErrRootDirUndefined,
			"",
			"root dir undefined",
		},
		{
			&S3Config{Root: "/foobar"},
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&S3Config{Root: "/foobar", Endpoint: "s3.example.foo"},
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&S3Config{Root: "/foobar", Endpoint: "s3.example.foo", AccessKey: "sekrit"},
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&S3Config{Root: "/foobar", Endpoint: "s3.example.foo", AccessKey: "sekrit", SecretKey: "sekrit"},
			nil,
			"S3 bucket foobar",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := initS3Fs(context.TODO(), tc.cfg)
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
