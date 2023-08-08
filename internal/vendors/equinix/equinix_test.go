package equinix

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseGithubReleaseURL(t *testing.T) {
	cases := []struct {
		name              string
		ghReleaseAssetURL string
		want              []string
	}{
		{
			"regular asset URL",
			"https://github.com/some-owner/some-repo/releases/download/some-tag/some-filename",
			[]string{"some-owner", "some-repo", "some-tag", "some-filename"},
		},
		{
			"broken asset URL",
			"https://github.com/some-repo/releases/download/some-tag/some-filename",
			[]string{"", "", "", ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, release, filename, err := parseGithubReleaseURL(tc.ghReleaseAssetURL)
			if err != nil {
				assert.ErrorContains(t, err, "parsing failed for URL path:")
				assert.Equal(t, tc.want[0], owner)
				assert.Equal(t, tc.want[1], repo)
				assert.Equal(t, tc.want[2], release)
				assert.Equal(t, tc.want[3], filename)
			} else {
				assert.Equal(t, tc.want[0], owner)
				assert.Equal(t, tc.want[1], repo)
				assert.Equal(t, tc.want[2], release)
				assert.Equal(t, tc.want[3], filename)
			}
		})
	}
}
