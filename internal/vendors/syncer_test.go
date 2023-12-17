package vendors

import (
	"context"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"fmt"
	"github.com/google/uuid"
	mockinventory "github.com/metal-toolbox/firmware-syncer/internal/inventory/mocks"
	"github.com/metal-toolbox/firmware-syncer/internal/logging"
	mockvendors "github.com/metal-toolbox/firmware-syncer/internal/vendors/mocks"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
	"os"
	"path"
	"strings"
	"time"
)

//go:generate mockgen -source=syncer_test.go -destination=mocks/rclone.go RCloneFS

// RCloneFS interface is just to help generate the mocks
type RCloneFS interface {
	fs.Fs
}

//go:generate mockgen -source=syncer_test.go -destination=mocks/rclone.go RCloneObject

// RCloneObject interface is just to help generate the mocks
type RCloneObject interface {
	fs.Object
}

//go:generate mockgen -source=syncer_test.go -destination=mocks/rclone.go RCloneInfo

// RCloneInfo interface is just to help generate the mocks
type RCloneInfo interface {
	fs.Info
}

type rootDirMatcher struct {
	root string
}

func (t *rootDirMatcher) Matches(x interface{}) bool {
	tempDir, ok := x.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(tempDir, t.root)
}

func (t *rootDirMatcher) String() string {
	return fmt.Sprintf("Does not have root dir %s", t.root)
}

func MatchesRootDir(root string) gomock.Matcher {
	return &rootDirMatcher{root: root}
}

func TestSyncer(t *testing.T) {
	logger := logging.NewLogger("debug")
	ctx := context.Background()
	tmpDir := os.TempDir()

	firmware := &serverservice.ComponentFirmwareVersion{
		UUID:          uuid.New(),
		Vendor:        "foo-vendor",
		Filename:      "foobar1.zip",
		Version:       "v0.0.0",
		Component:     "foo-component",
		Checksum:      "79ec3cf629b56317111d5640b8df1220", // real checksum of fixtures/foobar1.zip
		UpstreamURL:   "vendor-url",
		RepositoryURL: "repository-url",
	}

	firmwares := []*serverservice.ComponentFirmwareVersion{
		firmware,
	}

	tests := []struct {
		name            string
		fileShouldExist bool
		wantErr         assert.ErrorAssertionFunc
	}{
		{
			name:            "file does not exist",
			fileShouldExist: false,
			wantErr:         assert.NoError,
		},
		{
			name:            "file already exists",
			fileShouldExist: true,
			wantErr:         assert.NoError,
		},
	}

	localPath := path.Join("fixtures", firmware.Filename)
	dstPath := path.Join(firmware.Vendor, firmware.Filename)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockDstFs := mockvendors.NewMockRCloneFS(ctrl)
			mockTmpFs := mockvendors.NewMockRCloneFS(ctrl)
			mockDownloader := mockvendors.NewMockDownloader(ctrl)
			obj := mockvendors.NewMockRCloneObject(ctrl)

			if !tt.fileShouldExist {
				mockDownloader.EXPECT().
					Download(ctx, MatchesRootDir(tmpDir), firmware).
					Return(localPath, nil)

				mockDstFs.EXPECT().NewObject(ctx, dstPath).Return(nil, fs.ErrorObjectNotFound)

				info := mockvendors.NewMockRCloneInfo(ctrl)
				info.EXPECT().Precision().Return(time.Duration(0)).AnyTimes()

				obj.EXPECT().Size().Return(int64(0)).AnyTimes()
				obj.EXPECT().ModTime(ctx).Return(time.Now()).AnyTimes()
				obj.EXPECT().Fs().Return(info).AnyTimes()
				obj.EXPECT().String().Return("rclone-object").AnyTimes()

				mockDstFs.EXPECT().Root()
				mockDstFs.EXPECT().Name()

				mockTmpFs.EXPECT().NewObject(ctx, localPath).Return(obj, nil)
				mockTmpFs.EXPECT().Root().Return(tmpDir).AnyTimes()
				mockTmpFs.EXPECT().Name().Return("local").AnyTimes()
			}

			mockDstFs.EXPECT().NewObject(ctx, dstPath).Return(obj, nil).AnyTimes()

			mockInventory := mockinventory.NewMockServerService(ctrl)
			mockInventory.EXPECT().Publish(ctx, firmware)

			s := NewSyncer(
				mockDstFs,
				mockTmpFs,
				mockDownloader,
				mockInventory,
				firmwares,
				logger,
			)

			tt.wantErr(t, s.Sync(ctx), tt.name, "Syncer.Sync")
		})
	}
}
