package inventory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

func TestServerServicePublish(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc(
		"/api/v1/server-component-firmwares",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")

				_, _ = w.Write([]byte(`{}`))
			case http.MethodPost:
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}

				cfv := &serverservice.ComponentFirmwareVersion{}
				if err = json.Unmarshal(b, cfv); err != nil {
					t.Fatal(err)
				}

				// assert what we're publishing to serverservice is sane
				assert.Equal(t, "https://example.com/some/path/vendor/filename.zip", cfv.RepositoryURL)

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(b)
			default:
				t.Fatal("unexpected request method, got: " + r.Method)
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

	hss, err := New(context.TODO(), &cfg, artifactsURL, logger)
	if err != nil {
		t.Fatal(err)
	}

	cfv := serverservice.ComponentFirmwareVersion{
		Vendor:      "vendor",
		Model:       []string{"model1", "model2"},
		Filename:    "filename.zip",
		Version:     "1.2.3",
		Component:   "bmc",
		Checksum:    "1234",
		UpstreamURL: "http://some/location",
	}

	err = hss.Publish(context.TODO(), &cfv)
	if err != nil {
		t.Fatal(err)
	}
}
