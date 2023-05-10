package config

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func Test_LoadFirmwareManifest(t *testing.T) {
	intelE810 := `
[
	{
		"model": "E810",
		"manufacturer": "intel",
		"firmware": {
			"NIC": [
				{
					"build_date": "07/29/2022",
					"filename": "E810_NVMUpdatePackage_v4_00_Linux.tar.gz",
					"firmware_version": "4.00",
					"vendor_uri": "https://downloadmirror.intel.com/738712/E810_NVMUpdatePackage_v4_00.zip",
					"md5sum": "95cadf0842eb97cd29c3083362db0a35",
					"latest": true,
					"prerequisite": false,
					"note": "for in band update"
				}
			]
        }
    }
]
`

	dellR750ModelData := `
[
	{
		"model": "R750",
		"manufacturer": "dell",
		"firmware": {
            "StorageController": [
                {
                    "model": "HBA355i",
					"build_date": "11/09/2022",
					"filename": "SAS-Non-RAID_Firmware_2MHMF_WN64_22.15.05.00_A04.EXE",
					"firmware_version": "22.15.05.00",
					"vendor_uri": "https://dl.dell.com/FOLDER08925211M/1/SAS-Non-RAID_Firmware_2MHMF_WN64_22.15.05.00_A04.EXE",
					"md5sum": "b9f12aeec12b00ad5aea6e3b0fef7feb",
					"latest": true,
					"prerequisite": false
                }
			]
		}
    }
]
`

	dellR6415ModelData := `
[
	{
		"model": "R6415",
		"manufacturer": "dell",
		"firmware": {
			"StorageController": [
				{
                    "model": "BOSS-S1",
					"build_date": "08/10/2022",
					"filename": "SAS-RAID_Firmware_3P39V_WN64_2.5.13.3024_A07_02.EXE",
					"firmware_version": "2.5.13.3024",
					"vendor_uri": "https://dl.dell.com/FOLDER06189651M/3/SAS-RAID_Firmware_3P39V_WN64_2.5.13.3024_A07_02.EXE",
					"md5sum": "f9a156b4b077c826aa65eb8f1384efc3",
					"latest": true,
					"prerequisite": false
				}
			]
        }
    }
]
`
	cases := []struct {
		name              string
		modelData         string
		vendor            string
		expectedModels    []string
		expectedComponent string
	}{
		{
			"dell-boss-s1",
			dellR6415ModelData,
			"dell",
			[]string{"r6415", "boss-s1"},
			"storagecontroller",
		},
		{
			"dell-hba355i",
			dellR750ModelData,
			"dell",
			[]string{"r750", "hba355i"},
			"storagecontroller",
		},
		{
			"intel-e810",
			intelE810,
			"intel",
			[]string{"e810"},
			"nic",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.modelData)
			}))

			defer ts.Close()

			firmwaresByVendor, err := LoadFirmwareManifest(context.Background(), ts.URL)
			if err != nil {
				assert.EqualError(t, err, "Failed to load firmware manifest")
				return
			}

			for _, cfv := range firmwaresByVendor[tc.vendor] {
				assert.Equal(t, tc.expectedModels, cfv.Model)
				assert.Equal(t, tc.expectedComponent, cfv.Component)
			}
		})
	}
}
