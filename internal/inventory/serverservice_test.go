package inventory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
)

type testCase struct {
	name             string
	existingFirmware *serverservice.ComponentFirmwareVersion
	newFirmware      *serverservice.ComponentFirmwareVersion
	expectedFirmware *serverservice.ComponentFirmwareVersion
}

var idString = "e2458c5e-bf0b-11ee-815a-f76c5993e3ca"

func TestServerServicePublish(t *testing.T) {
	id, err := uuid.Parse(idString)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []*testCase{
		{
			"Post New Firmware",
			nil,
			&serverservice.ComponentFirmwareVersion{
				Vendor:      "vendor",
				Model:       []string{"model1", "model2"},
				Filename:    "filename.zip",
				Version:     "1.2.3",
				Component:   "bmc",
				Checksum:    "1234",
				UpstreamURL: "http://some/location",
			},
			&serverservice.ComponentFirmwareVersion{
				Vendor:        "vendor",
				Model:         []string{"model1", "model2"},
				Filename:      "filename.zip",
				Version:       "1.2.3",
				Component:     "bmc",
				Checksum:      "1234",
				UpstreamURL:   "http://some/location",
				RepositoryURL: "https://example.com/some/path/vendor/filename.zip",
			},
		},
		{
			"Existing Firmware",
			&serverservice.ComponentFirmwareVersion{
				UUID:          id,
				Vendor:        "vendor",
				Model:         []string{"model1", "model2"},
				Filename:      "filename.zip",
				Version:       "1.2.3",
				Component:     "bmc",
				Checksum:      "1234",
				UpstreamURL:   "http://some/location",
				RepositoryURL: "https://example.com/some/path/vendor/filename.zip",
			},
			&serverservice.ComponentFirmwareVersion{
				Vendor:      "vendor",
				Model:       []string{"model2"},
				Filename:    "filename.zip",
				Version:     "1.2.3",
				Component:   "bmc",
				Checksum:    "1234",
				UpstreamURL: "http://some/location",
			},
			nil,
		},
		{
			"Update existing Firmware",
			&serverservice.ComponentFirmwareVersion{
				UUID:          id,
				Vendor:        "vendor",
				Model:         []string{"model1", "model3"},
				Filename:      "filename.zip",
				Version:       "1.2.3",
				Component:     "bmc",
				Checksum:      "1234",
				UpstreamURL:   "http://some/location",
				RepositoryURL: "https://example.com/some/path/vendor/filename.zip",
			},
			&serverservice.ComponentFirmwareVersion{
				Vendor:      "vendor",
				Model:       []string{"model2", "model4"},
				Filename:    "filename.zip",
				Version:     "1.2.4",
				Component:   "bmc",
				Checksum:    "1234",
				UpstreamURL: "http://some/location",
			},
			&serverservice.ComponentFirmwareVersion{
				UUID:          id,
				Vendor:        "vendor",
				Model:         []string{"model1", "model2", "model3", "model4"},
				Filename:      "filename.zip",
				Version:       "1.2.4",
				Component:     "bmc",
				Checksum:      "1234",
				UpstreamURL:   "http://some/location",
				RepositoryURL: "https://example.com/some/path/vendor/filename.zip",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			testServerServicePublish(t, tt)
		})
	}
}

func testServerServicePublish(t *testing.T, tt *testCase) {
	handler := http.NewServeMux()
	handler.HandleFunc(
		"/api/v1/server-component-firmwares",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")

				serverResponse := &serverservice.ServerResponse{}

				if tt.existingFirmware != nil {
					serverResponse.Records = []*serverservice.ComponentFirmwareVersion{
						tt.existingFirmware,
					}
				}

				responseBytes, err := json.Marshal(serverResponse)
				if err != nil {
					t.Fatal(err)
				}

				if _, err = w.Write(responseBytes); err != nil {
					t.Fatal(err)
				}
			case http.MethodPost:
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}

				newFirmware := &serverservice.ComponentFirmwareVersion{}
				if err = json.Unmarshal(b, newFirmware); err != nil {
					t.Fatal(err)
				}

				if tt.expectedFirmware != nil {
					assert.Equal(t, tt.expectedFirmware, newFirmware)
				} else {
					t.Fatal("No firmware POST expected")
				}

				w.Header().Set("Content-Type", "application/json")

				if _, err = w.Write(b); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("unexpected request method, got: " + r.Method)
			}
		},
	)
	handler.HandleFunc(
		"/api/v1/server-component-firmwares/"+idString,
		func(writer http.ResponseWriter, request *http.Request) {
			switch request.Method {
			case http.MethodPut:
				b, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}

				updatedFirmware := &serverservice.ComponentFirmwareVersion{}
				if err = json.Unmarshal(b, updatedFirmware); err != nil {
					t.Fatal(err)
				}

				if tt.expectedFirmware != nil {
					assert.Equal(t, tt.expectedFirmware, updatedFirmware)
				} else {
					t.Fatal("No firmware PUT expected")
				}

				writer.Header().Set("Content-Type", "application/json")

				if _, err = writer.Write(b); err != nil {
					t.Fatal(err)
				}
			}
		},
	)

	mock := httptest.NewServer(handler)
	defer mock.Close()

	cfg := config.ServerserviceOptions{
		Endpoint:     mock.URL,
		DisableOAuth: true,
	}

	artifactsURL := "https://example.com/some/path"

	logger := logrus.New()
	logger.Out = io.Discard

	hss, err := New(context.Background(), &cfg, artifactsURL, logger)
	if err != nil {
		t.Fatal(err)
	}

	err = hss.Publish(context.Background(), tt.newFirmware)
	if err != nil {
		t.Fatal(err)
	}
}
