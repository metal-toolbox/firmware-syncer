package supermicro

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getChecksumFilename(t *testing.T) {
	checksumFileExample1 := `
softfiles/14021/BMC_X11AST2500-4101MS_20210510_01.73.12_STDsp.zip
CRC32 CheckSum: 5d32ec4b
MD5 CheckSum: 1a18d5d94fad55dc6fc51630383b1e7f
`
	checksumFileExample2 := `
softfiles/14075/BIOS_X11SCH-F-1B11_20210525_1.6_STDsp.zip
CRC32 CheckSum: d9f797b8
MD5 CheckSum: 9cd49a78f10d513f43f861e674d51c10

softfiles/14075/X11SCH-(LN4)F_BIOS_1.6_release_notes.pdf
CRC32 CheckSum: daedfe3b
MD5 CheckSum: 3f5cecadf92192d86d049a99b36939ab

`
	checksumFileExample3 := `
/softfiles/4390/SMT_MBIPMI_339_REDFISH.zip MD5 = 33cdcd726f36f8ac35d8a0e4cea4a2a8
/softfiles/4390/SMT_MBIPMI_339_REDFISH.zip SHA1 = 103a717fbaf3b88f23e64e7bfe81e97ce2af10c3
`
	checksumFileExample4 := `
/softfiles/MD5
`
	cases := []struct {
		name         string
		checksumFile io.Reader
		wantChecksum string
		wantFilename string
	}{
		{
			"checksumFileExample1",
			strings.NewReader(checksumFileExample1),
			"1a18d5d94fad55dc6fc51630383b1e7f",
			"BMC_X11AST2500-4101MS_20210510_01.73.12_STDsp.zip",
		},
		{
			"checksumFileExample2",
			strings.NewReader(checksumFileExample2),
			"9cd49a78f10d513f43f861e674d51c10",
			"BIOS_X11SCH-F-1B11_20210525_1.6_STDsp.zip",
		},
		{
			"checksumFileExample3",
			strings.NewReader(checksumFileExample3),
			"33cdcd726f36f8ac35d8a0e4cea4a2a8",
			"SMT_MBIPMI_339_REDFISH.zip",
		},
		{"checksumFileExample4",
			strings.NewReader(checksumFileExample4),
			"",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename, checksum, err := parseFilenameAndChecksum(tc.checksumFile)
			if err != nil {
				assert.ErrorContains(t, err, "parsing failed: runtime error:")
				assert.Equal(t, tc.wantFilename, filename)
				assert.Equal(t, tc.wantChecksum, checksum)
			}
			assert.Equal(t, tc.wantFilename, filename)
			assert.Equal(t, tc.wantChecksum, checksum)
		})
	}
}

func Test_extractFirmware(t *testing.T) {
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
			f, err := extractFirmware(tc.archivePath, tc.firmwareFilename, tc.firmwareChecksum)
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
