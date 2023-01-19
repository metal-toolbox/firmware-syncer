package vendors

import (
	"context"
	"errors"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/stretchr/testify/assert"
)

func Test_InitLocalFs(t *testing.T) {
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
			f, err := InitLocalFs(context.TODO(), tc.cfg)
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

func Test_InitS3Fs(t *testing.T) {
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
			f, err := InitS3Fs(context.TODO(), tc.cfg, tc.root)
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
