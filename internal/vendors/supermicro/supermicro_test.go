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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename, checksum, _ := parseFilenameAndChecksum(tc.checksumFile)
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
			// Archive:  X11DPT-B(H)_3_4_AS17305_SUM250.zip
			//  Length      Date    Time    Name
			//  ---------  ---------- -----   ----
			//		  0  11-23-2020 10:18   X11DPT-B(H)_3.4_AS17305_SUM250/
			//   29625347  09-21-2020 11:52   X11DPT-B(H)_3.4_AS17305_SUM250/SMT_X11AST2500_173_05.zip
			//   19892213  09-08-2020 11:00   X11DPT-B(H)_3.4_AS17305_SUM250/sum_2.5.0_BSD_x86_64_20200722.tar.gz
			//   11280455  09-08-2020 11:00   X11DPT-B(H)_3.4_AS17305_SUM250/sum_2.5.0_Linux_x86_64_20200722.tar.gz
			//	8952902  09-08-2020 11:00   X11DPT-B(H)_3.4_AS17305_SUM250/sum_2.5.0_Win_x86_64_20200722.zip
			//	   2422  11-23-2020 10:19   X11DPT-B(H)_3.4_AS17305_SUM250/X11DPT-B(H)_Software_Package_Readme.txt
			//   10455672  11-03-2020 09:34   X11DPT-B(H)_3.4_AS17305_SUM250/X11DPTB0.B03.zip
			//  ---------                     -------
			//   80209011                     7 files

		},
		{
			// foobar4.zip
			//  |-foo.bar
			"firmware without bin extension",
			getPathToFixture("foobar4.zip"),
			"foo.bar",
			"14758f1afd44c09b7992073ccf00b43d",
			// Archive:  H12SSW0_528.zip
			//  Length      Date    Time    Name
			//  ---------  ---------- -----   ----
			//	 570480  05-03-2019 15:21   afuefi.smc
			//	   1149  02-07-2018 19:56   flash.nsh
			//   33554432  05-28-2020 14:35   H12SSW0.528
			//	   1629  06-21-2019 14:24   Readme for H12 AMI BIOS-UEFI.txt
			//  ---------                     -------
			//   34127690                     4 files
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
			os.Remove(f.Name())
		})
	}
}

func getPathToFixture(fixture string) string {
	p, _ := filepath.Abs(fmt.Sprintf("fixtures/%s", fixture))
	return p
}
