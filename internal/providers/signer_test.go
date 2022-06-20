package providers

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	pubKeyFile = "/tmp/foo.pub"
	pvtKeyFile = "/tmp/foo.key"
	sigFile    = "/tmp/foo.pub.sig"
)

var (

	// signature of dummyPublicKey file contents
	dummySig = []byte(`untrusted comment: signature from signify secret key
RWQpEG27G48W0Ur6Gj2SBGcyoRnLP8HTUYDpaWM1lWVf2AlnzZQ9X1eU/fXPhv//UkWx5z9JVCTVXjj6ATu8BS09EALTgNGfqgM=
`)

	dummyPublicKey = []byte(`untrusted comment: signify public key
RWQpEG27G48W0baksrm14Jj5cQ9Pca9I/nHSW2owsAfbsMCYTi39g7Rk
`)

	dummyPrivateKey = []byte(`untrusted comment: signify secret key
RWRCSwAAAADnjmCkn8Ra8JIaAxosfAFtmLDDb/NtYvApEG27G48W0ao2IePTZgKYU4BpjIt4fVfeyXJAF7EGkWAHbKE2NAHctqSyubXgmPlxD09xr0j+cdJbajCwB9uwwJhOLf2DtGQ=
`)

	dummyKeyFiles = map[string][]byte{pubKeyFile: dummyPublicKey, pvtKeyFile: dummyPrivateKey}
)

// nolint:gocritic // testcode
// helper methods
func createKeyFiles(t *testing.T) {
	for k, v := range dummyKeyFiles {
		err := os.WriteFile(k, v, 0600)
		if err != nil {
			t.Error(err)
		}
	}
}

// nolint:gocritic // testcode
func createSigFile(t *testing.T) {
	err := os.WriteFile(sigFile, dummySig, 0600)
	if err != nil {
		t.Error(err)
	}
}

func purgeFiles(files []string, t *testing.T) {
	for _, k := range files {
		_, err := os.Stat(k)
		if err != nil {
			continue
		}

		err = os.Remove(k)
		if err != nil {
			t.Error(err)
		}
	}
}

// nolint:gocritic // testcode
func Test_NewSigner(t *testing.T) {
	cases := []struct {
		pvtKeyFile  string
		pubKeyFile  string
		createFiles bool
		err         error
	}{
		{
			pvtKeyFile,
			pubKeyFile,
			true,
			nil,
		},
		{
			pvtKeyFile,
			"",
			false,
			ErrSignerPubKey,
		},
	}

	for _, tt := range cases {
		if tt.createFiles {
			// write dummy pub files
			createKeyFiles(t)
			createSigFile(t)

			defer purgeFiles([]string{tt.pvtKeyFile, tt.pubKeyFile}, t)
		}

		s, err := NewSigner(tt.pvtKeyFile, tt.pubKeyFile)
		if tt.err != nil {
			assert.True(t, errors.Is(tt.err, err))
			continue
		}

		assert.Nil(t, err)
		assert.Equal(t, tt.pubKeyFile, s.publicKeyFile)
		assert.Equal(t, tt.pvtKeyFile, s.privateKeyFile)
	}
}

// nolint:gocritic // testcode
func Test_Sign(t *testing.T) {
	cases := []struct {
		pvtKeyFile     string
		pubKeyFile     string
		targetFile     string
		sigFile        string
		createKeyFiles bool
		err            error
	}{
		{
			// no private key parameter returns error
			"",
			pubKeyFile,
			"",
			"",
			true,
			ErrSignerPrivateKey,
		},
		{
			// sign
			pvtKeyFile,
			pubKeyFile,
			pubKeyFile,
			sigFile,
			true,
			nil,
		},
		{
			// sign with sig file name
			pvtKeyFile,
			pubKeyFile,
			pubKeyFile,
			"/tmp/foo.pub.customsig",
			true,
			nil,
		},
	}
	for _, tt := range cases {
		if tt.createKeyFiles {
			// write dummy pub files
			createSigFile(t)
			createKeyFiles(t)

			defer purgeFiles([]string{tt.pvtKeyFile, tt.pubKeyFile, tt.sigFile}, t)
		}

		s, err := NewSigner(tt.pvtKeyFile, tt.pubKeyFile)
		if err != nil {
			t.Error(err)
		}

		err = s.Sign(tt.targetFile, tt.sigFile)
		if tt.err != nil {
			assert.True(t, errors.Is(tt.err, err))
			continue
		}

		assert.Nil(t, err)

		if tt.sigFile != "" {
			assert.FileExists(t, tt.sigFile)

			b, err := os.ReadFile(tt.sigFile)
			if err != nil {
				t.Error(err)
			}

			assert.Equal(t, dummySig, b)
		}
	}
}

func Test_Verify(t *testing.T) {
	cases := []struct {
		pvtKeyFile string
		pubKeyFile string
		targetFile string
		sigFile    string
		err        error
	}{
		{
			// verify
			pvtKeyFile,
			"",
			pubKeyFile,
			sigFile,
			ErrSignerPubKey,
		},
		{
			// verify
			pvtKeyFile,
			pubKeyFile,
			pubKeyFile,
			sigFile,
			nil,
		},
	}

	createSigFile(t)
	createKeyFiles(t)

	defer purgeFiles([]string{pvtKeyFile, pubKeyFile, sigFile}, t)

	for _, tt := range cases {
		s, err := NewSigner(tt.pvtKeyFile, tt.pubKeyFile)
		if tt.err != nil {
			assert.ErrorIs(t, err, tt.err)
			continue
		}

		if err != nil {
			t.Fatal(err)
		}

		err = s.Verify(tt.targetFile, tt.sigFile)
		if tt.err != nil {
			assert.ErrorIs(t, err, tt.err)
			continue
		}

		assert.Nil(t, err)

		if tt.sigFile != "" {
			assert.FileExists(t, tt.sigFile)

			b, err := os.ReadFile(tt.sigFile)
			if err != nil {
				t.Error(err)
			}

			assert.Equal(t, dummySig, b)
		}
	}
}
