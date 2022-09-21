package asrockrack

import (
	"context"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/stretchr/testify/assert"
)

func Test_initDownloader(t *testing.T) {
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
			"ASRockRack downloader initialization",
			srcConfig,
			dstConfig,
			"src-bucket",
			"dst-bucket",
			nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := initDownloader(context.TODO(), tc.srcCfg, tc.dstCfg)
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
