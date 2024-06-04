package vendors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
	"go.uber.org/mock/gomock"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	mock_vendors "github.com/metal-toolbox/firmware-syncer/internal/vendors/mocks"
)

func Test_InitLocalFs(t *testing.T) {
	cases := []struct {
		cfg  *LocalFsConfig
		err  error
		want string
		name string
	}{
		{
			nil,
			ErrFileStoreConfig,
			"",
			"fs config nil",
		},
		{
			&LocalFsConfig{},
			ErrRootDirUndefined,
			"",
			"root dir undefined",
		},
		{
			&LocalFsConfig{Root: "/foobar"},
			nil,
			"Local file system at /foobar",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := InitLocalFs(context.TODO(), tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.Nil(t, err)
			if tc.want != "" {
				assert.Equal(t, tc.want, f.String())
			}
		})
	}
}

func Test_InitS3Fs(t *testing.T) {
	cases := []struct {
		cfg  *config.S3Bucket
		root string
		err  error
		want string
		name string
	}{
		{
			nil,
			"",
			ErrFileStoreConfig,
			"",
			"s3 config nil",
		},
		{
			&config.S3Bucket{},
			"",
			ErrRootDirUndefined,
			"",
			"root dir undefined",
		},
		{
			&config.S3Bucket{},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Endpoint: "s3.example.foo"},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Endpoint: "s3.example.foo", AccessKey: "sekrit"},
			"/foobar",
			ErrInitS3Fs,
			"",
			"s3 params undefined",
		},
		{
			&config.S3Bucket{Region: "region", Endpoint: "s3.example.foo", AccessKey: "sekrit", SecretKey: "sekrit"},
			"/foobar",
			nil,
			"S3 bucket foobar",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := InitS3Fs(context.TODO(), tc.cfg, tc.root)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.Nil(t, err)
			if tc.want != "" {
				assert.Equal(t, tc.want, f.String())
			}
		})
	}
}

func Test_SplitURLPath(t *testing.T) {
	cases := []struct {
		httpURL  string
		hostPart string
		urlPart  string
		err      error
	}{
		{
			"https://example.com/foo/bar",
			"https://example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://example.com/foo/bar",
			"http://example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://example.com/foo/bar/",
			"http://example.com",
			"/foo/bar/",
			nil,
		},
		{
			"http://user:pass@example.com/foo/bar",
			"http://user:pass@example.com",
			"/foo/bar",
			nil,
		},
		{
			"http://user:pass@example.com/foo/bar?foo=baz&lala=112",
			"http://user:pass@example.com",
			"/foo/bar?foo=baz&lala=112",
			nil,
		},
		{
			"file://example.com/foo/bar",
			"",
			"",
			ErrURLUnsupported,
		},
		{
			"https://example.com/foo/bar/foo.bin",
			"https://example.com",
			"/foo/bar/foo.bin",
			nil,
		},
		{
			"https://user:pass@example.com/foo/bar/foo.bin",
			"https://user:pass@example.com",
			"/foo/bar/foo.bin",
			nil,
		},
	}

	for _, tt := range cases {
		hostPart, urlPart, err := SplitURLPath(tt.httpURL)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
		} else {
			assert.Nil(t, err)
		}

		assert.Equal(t, tt.hostPart, hostPart)
		assert.Equal(t, tt.urlPart, urlPart)
	}
}

//go:generate mockgen -source=downloader_test.go -destination=mocks/httpDoer.go HTTPDoer

// HTTPDoer interface is meant to help generate the mock
type HTTPDoer interface {
	serverservice.Doer
}

// URL Matcher

type requestURLMatcher struct {
	expectedURL string
}

func matchesURL(expectedURL string) *requestURLMatcher {
	return &requestURLMatcher{expectedURL: expectedURL}
}

func (v *requestURLMatcher) Matches(i interface{}) bool {
	request, ok := i.(*http.Request)
	if !ok {
		return false
	}

	return request.URL.String() == v.expectedURL
}

func (v *requestURLMatcher) String() string {
	return fmt.Sprintf("expected URL %s", v.expectedURL)
}

// ReadCloser Error

type readCloserErr struct{}

func (r *readCloserErr) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (r *readCloserErr) Close() error {
	return nil
}

// SourceOverrideDownloader Test

func Test_SourceOverrideDownloader(t *testing.T) {
	ctx := context.Background()
	logger := logrus.New()

	testCases := []struct {
		name            string
		statusCode      int
		withBadURL      bool
		withClientError bool
		withCopyError   bool
		expectedError   error
	}{
		{
			name: "success",
		},
		{
			name:          "bad url",
			withBadURL:    true,
			expectedError: ErrSourceURL,
		},
		{
			name:            "client error",
			withClientError: true,
			expectedError:   ErrDownloadingFile,
		},
		{
			name:          "bad status code",
			statusCode:    500,
			expectedError: ErrUnexpectedStatusCode,
		},
		{
			name:          "copy error",
			withCopyError: true,
			expectedError: ErrCopy,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp(os.TempDir(), "test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			statusCode := 200
			if tt.statusCode != 0 {
				statusCode = tt.statusCode
			}

			var body io.ReadCloser = &http.NoBody
			if tt.withCopyError {
				body = &readCloserErr{}
			}

			fakeResponse := &http.Response{Body: body, StatusCode: statusCode}

			ctrl := gomock.NewController(t)
			client := mock_vendors.NewMockHTTPDoer(ctrl)

			var clientError error
			if tt.withClientError {
				clientError = io.ErrUnexpectedEOF
			}

			fakeURL := "https://foo"
			firmwareName := "firmware.bin"

			if tt.withBadURL {
				fakeURL = "!@#$%^&*()_+-="
			} else {
				client.EXPECT().Do(matchesURL("https://foo/firmware.bin")).Return(fakeResponse, clientError)
			}

			fakeFirmware := &serverservice.ComponentFirmwareVersion{Filename: firmwareName}
			downloader := NewSourceOverrideDownloader(logger, client, fakeURL)
			firmwarePath, err := downloader.Download(ctx, tmpDir, fakeFirmware)

			if tt.expectedError != nil {
				assert.ErrorContains(t, err, tt.expectedError.Error())
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, path.Join(tmpDir, firmwareName), firmwarePath)
			assert.FileExists(t, firmwarePath)
		})
	}
}
