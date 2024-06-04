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
	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
)

type testCase struct {
	name             string
	existingFirmware *fleetdbapi.ComponentFirmwareVersion
	newFirmware      *fleetdbapi.ComponentFirmwareVersion
	expectedFirmware *fleetdbapi.ComponentFirmwareVersion
}

var idString = "e2458c5e-bf0b-11ee-815a-f76c5993e3ca"
var artifactsURL = "https://example.com/some/path"

func TestServerServicePublish(t *testing.T) {
	id, err := uuid.Parse(idString)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []*testCase{
		{
			"Post New Firmware",
			nil,
			&fleetdbapi.ComponentFirmwareVersion{
				Vendor:      "vendor",
				Model:       []string{"model1", "model2"},
				Filename:    "filename.zip",
				Version:     "1.2.3",
				Component:   "bmc",
				Checksum:    "1234",
				UpstreamURL: "http://some/location",
			},
			&fleetdbapi.ComponentFirmwareVersion{
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
			&fleetdbapi.ComponentFirmwareVersion{
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
			&fleetdbapi.ComponentFirmwareVersion{
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
			&fleetdbapi.ComponentFirmwareVersion{
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
			&fleetdbapi.ComponentFirmwareVersion{
				Vendor:      "vendor",
				Model:       []string{"model2", "model4"},
				Filename:    "filename.zip",
				Version:     "1.2.4",
				Component:   "bmc",
				Checksum:    "1234",
				UpstreamURL: "http://some/location",
			},
			&fleetdbapi.ComponentFirmwareVersion{
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

func handleGetFirmware(t *testing.T, tt *testCase, writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json")

	serverResponse := &fleetdbapi.ServerResponse{}

	if tt.existingFirmware != nil {
		serverResponse.Records = []*fleetdbapi.ComponentFirmwareVersion{
			tt.existingFirmware,
		}
	}

	responseBytes, err := json.Marshal(serverResponse)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = writer.Write(responseBytes); err != nil {
		t.Fatal(err)
	}
}

func handleUpdateFirmware(t *testing.T, tt *testCase, writer http.ResponseWriter, request *http.Request) {
	b, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}

	newFirmware := &fleetdbapi.ComponentFirmwareVersion{}
	if err = json.Unmarshal(b, newFirmware); err != nil {
		t.Fatal(err)
	}

	if tt.expectedFirmware != nil {
		assert.Equal(t, tt.expectedFirmware, newFirmware)
	} else {
		t.Fatalf("No firmware %s expected", request.Method)
	}

	writer.Header().Set("Content-Type", "application/json")

	if _, err = writer.Write(b); err != nil {
		t.Fatal(err)
	}
}

func newHandler(t *testing.T, tt *testCase) *http.ServeMux {
	handler := http.NewServeMux()

	handler.HandleFunc(
		"/api/v1/server-component-firmwares",
		func(writer http.ResponseWriter, request *http.Request) {
			switch request.Method {
			case http.MethodGet:
				handleGetFirmware(t, tt, writer)
			case http.MethodPost:
				handleUpdateFirmware(t, tt, writer, request)
			default:
				t.Fatal("unexpected request method, got: " + request.Method)
			}
		},
	)

	handler.HandleFunc(
		"/api/v1/server-component-firmwares/"+idString,
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPut {
				handleUpdateFirmware(t, tt, writer, request)
			} else {
				t.Fatal("unexpected request method, got: " + request.Method)
			}
		},
	)

	return handler
}

func testServerServicePublish(t *testing.T, tt *testCase) {
	handler := newHandler(t, tt)

	mock := httptest.NewServer(handler)
	defer mock.Close()

	cfg := config.ServerserviceOptions{
		Endpoint:     mock.URL,
		DisableOAuth: true,
	}

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
