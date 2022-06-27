package dell

import (
	"context"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
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
			"DUP downloader initialization",
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
