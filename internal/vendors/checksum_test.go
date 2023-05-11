package vendors

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// nolint:gocritic // testcode
func Test_SHA256Checksum(t *testing.T) {
	testfile := "/tmp/foo.blah"
	cases := []struct {
		filename string
		checksum []byte
		err      error
	}{
		{
			"",
			nil,
			ErrChecksumGenerate,
		},
		{
			testfile,
			[]byte(`97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5`),
			nil,
		},
	}

	for _, tt := range cases {
		if tt.filename != "" {
			_, err := os.Create(tt.filename)
			if err != nil {
				t.Error(err)
			}

			err = os.WriteFile(testfile, []byte(`checksum this`), 0600)
			if err != nil {
				t.Error(err)
			}

			defer os.Remove(tt.filename)
			defer os.Remove(tt.filename + SumSuffix)
		}

		err := SHA256Checksum(tt.filename)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
			continue
		}

		var b []byte
		if tt.checksum != nil {
			b, err = os.ReadFile(tt.filename + SumSuffix)
			if err != nil {
				t.Error(err)
			}

			assert.Equal(t, tt.checksum, b)
		}

		assert.Nil(t, err)
	}
}

// nolint:gocritic // testcode
func Test_SHA256ChecksumValidate(t *testing.T) {
	testfile := "/tmp/foo.blah"
	cases := []struct {
		filename         string
		checksum         string
		expectedChecksum []byte
		createFile       bool
		err              error
	}{
		{
			"",
			"",
			nil,
			false,
			ErrChecksumValidate,
		},
		{
			testfile,
			"",
			nil,
			true,
			nil,
		},
		{
			testfile,
			"invalid checksum",
			[]byte(`97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5`),
			true,
			ErrChecksumInvalid,
		},
		{
			testfile,
			"97E9269CD0514F864E6BE9157998464C94776EBC7F669B449F581ABDAD4035F5",
			[]byte(`97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5`),
			true,
			nil,
		},
		{
			testfile,
			"97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5",
			[]byte(`97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5`),
			true,
			nil,
		},
	}

	for _, tt := range cases {
		if tt.createFile {
			_, err := os.Create(testfile)
			if err != nil {
				t.Error(err)
			}

			err = os.WriteFile(testfile, []byte(`checksum this`), 0600)
			if err != nil {
				t.Error(err)
			}

			defer os.Remove(testfile)
			defer os.Remove(testfile + SumSuffix)

			err = SHA256Checksum(tt.filename)
			if err != nil {
				t.Error(err)
				continue
			}
		}

		err := SHA256ChecksumValidate(tt.filename, tt.checksum)
		if tt.err != nil {
			assert.True(t, errors.Is(err, tt.err))
			continue
		}

		assert.Nil(t, err)
	}
}

func Test_ValidateChecksum(t *testing.T) {
	testfile := "/tmp/foo.blah"
	cases := []struct {
		name     string
		filename string
		checksum string
	}{
		{
			"nohint",
			testfile,
			"803ac72f8be2eba9f985fd3be31b506c",
		},
		{
			"sha256",
			testfile,
			"sha256:97e9269cd0514f864e6be9157998464c94776ebc7f669b449f581abdad4035f5",
		},
		{
			"md5sum",
			testfile,
			"md5sum:803ac72f8be2eba9f985fd3be31b506c",
		},
	}

	for _, tt := range cases {
		_, err := os.Create(tt.filename)
		if err != nil {
			t.Error(err)
		}

		err = os.WriteFile(testfile, []byte(`checksum this`), 0o0600)
		if err != nil {
			t.Error(err)
		}

		// nolint:gocritic
		defer os.Remove(tt.filename)

		assert.True(t, ValidateChecksum(tt.filename, tt.checksum))
	}
}
