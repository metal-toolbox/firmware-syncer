package dell

import (
	"context"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/providers"
	"github.com/stretchr/testify/assert"

	"github.com/sirupsen/logrus"
)

func Test_initDownloaderDUP(t *testing.T) {
	vendor := "dell"
	logger := &logrus.Logger{}
	cfg := &config.S3Bucket{
		Region:    "region",
		SecretKey: "foo",
		AccessKey: "bar",
		Endpoint:  "endpoint",
		Bucket:    "stuff",
	}

	cases := []struct {
		srcURL     string
		cfg        *config.S3Bucket
		wantSrcURL string
		wantDstURL string
		err        error
		name       string
	}{
		{
			"https://foo/bar/baz.bin",
			cfg,
			"https://foo/bar/baz.bin",
			"",
			nil,
			"DUP downloader initialization",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := providers.NewDownloader(context.TODO(), vendor, tc.srcURL, tc.cfg, logger)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}

			assert.Nil(t, err)

			if tc.wantSrcURL != "" {
				assert.Equal(t, tc.wantSrcURL, got.SrcURL())
			}

			if tc.wantDstURL != "" {
				assert.Equal(t, tc.wantDstURL, got.DstURL())
			}
		})
	}
}
