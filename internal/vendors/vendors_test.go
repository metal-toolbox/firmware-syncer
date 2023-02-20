package vendors

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_TransferMetrics(t *testing.T) {
	cases := []struct {
		int64Key   string
		int64Value int64
		expected   map[string]int64
		clear      bool
		name       string
	}{
		{
			MetricTransferredBytes,
			100,
			map[string]int64{MetricTransferredBytes: 100},
			false,
			"new int64 key, value add",
		},
		{
			MetricTransferredBytes,
			100,
			map[string]int64{MetricTransferredBytes: 200},
			false,
			"int64 key, value increment",
		},
		{
			"",
			0,
			map[string]int64{},
			true,
			"clear values",
		},
	}

	metrics := NewMetrics()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.clear {
				metrics.Clear()
			} else {
				metrics.AddInt64Value(tc.int64Key, tc.int64Value)
			}
			assert.Equal(t, tc.expected, metrics.int64Values)
		})
	}
}

func Test_ExtractFromZipArchive(t *testing.T) {
	cases := []struct {
		name             string
		archivePath      string
		firmwareFilename string
		firmwareChecksum string
	}{
		{
			// foobar1.zip
			//  |-foobar1.bin
			"archive name matches firmware name",
			getPathToFixture("foobar1.zip"),
			"foobar1.bin",
			"14758f1afd44c09b7992073ccf00b43d",
		},
		{
			// foobar2.zip
			//  |-foobar/foobar.bin
			"firmware file inside dir in archive",
			getPathToFixture("foobar2.zip"),
			"foobar.bin",
			"14758f1afd44c09b7992073ccf00b43d",
		},
		{
			// foobar3.zip
			//  |-foobar/foobar.zip
			"nested zip firmware file",
			getPathToFixture("foobar3.zip"),
			"foobar.bin",
			"14758f1afd44c09b7992073ccf00b43d",
		},
		{
			// foobar4.zip
			//  |-foo.bar
			"firmware without bin extension",
			getPathToFixture("foobar4.zip"),
			"foo.bar",
			"14758f1afd44c09b7992073ccf00b43d",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := ExtractFromZipArchive(tc.archivePath, tc.firmwareFilename, tc.firmwareChecksum)
			if err != nil {
				assert.EqualError(t, err, "some error")
				return
			}
			assert.Equal(t, tc.firmwareFilename, filepath.Base(f.Name()))
			// Remove the unzipped file from the filesystem
			defer os.Remove(f.Name())
		})
	}
}

func getPathToFixture(fixture string) string {
	p, _ := filepath.Abs(fmt.Sprintf("fixtures/%s", fixture))
	return p
}
