package vendors

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/pkg/errors"
)

const (
	SumSuffix = ".SHA256"
)

var (
	ErrChecksumGenerate = errors.New("error generate file checksum")
	ErrChecksumValidate = errors.New("error validating file checksum")
	ErrChecksumInvalid  = errors.New("file checksum does not match")
)

// SHA256FileChecksum calculates the sha256 checksum of the given filename
// and writes a filename.SHA256 file with its checksum value
func SHA256Checksum(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return errors.Wrap(ErrChecksumGenerate, err.Error())
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.Wrap(err, ErrChecksumGenerate.Error())
	}

	sum := fmt.Sprintf("%x", h.Sum(nil))

	return os.WriteFile(filename+SumSuffix, []byte(sum), 0o600)
}

// SHA256FileChecksumValidate verifies the sha256 checksum of the given filename
//
// If a checksum parameter is provided, the method compares the file checksum with the one provided.
// If no checksum  parameter was given, the method looks for 'filename.SHA256' to read the checksum to validate.
// when the checksum does not match the expected, an error is returned
func SHA256ChecksumValidate(filename, checksum string) error {
	var expectedChecksum []byte

	var err error

	if filename == "" {
		return errors.Wrap(ErrChecksumValidate, "expected a filename to validate checksum")
	}

	// attempt to read .SHA256 file when a checksum isn't specified in the parameter
	if checksum == "" {
		// read file containing checksum
		expectedChecksum, err = os.ReadFile(filename + SumSuffix)
		if err != nil {
			return errors.Wrap(ErrChecksumValidate, err.Error()+filename+SumSuffix)
		}
	} else {
		expectedChecksum = []byte(strings.ToLower(checksum))
	}

	// calculate checksum for filename
	f, err := os.Open(filename)
	if err != nil {
		return errors.Wrap(ErrChecksumValidate, err.Error()+filename)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.Wrap(ErrChecksumValidate, err.Error())
	}

	calculatedChecksum := fmt.Sprintf("%x", h.Sum(nil))
	if !bytes.Equal(expectedChecksum, []byte(calculatedChecksum)) {
		errMsg := fmt.Sprintf(
			"filename: %s expected: %s, got: %s",
			filename,
			string(expectedChecksum),
			string(calculatedChecksum),
		)

		return errors.Wrap(ErrChecksumInvalid, errMsg)
	}

	return nil
}

func validateSHA256Checksum(filename, checksum string) bool {
	err := SHA256ChecksumValidate(filename, checksum)
	return err == nil
}

func validateMD5Checksum(filename, checksum string) bool {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
		return false
	}
	defer f.Close()

	h := md5.New()

	_, err = io.Copy(h, f)
	if err != nil {
		return false
	}

	return checksum == hex.EncodeToString(h.Sum(nil))
}

// ValidateChecksum validates the file checksum matches the given value.
// Defaults to md5 but allows for sha256 checks
func ValidateChecksum(filename, checksum string) bool {
	// checksum format <hint>:<checksum>
	splittedChecksum := strings.Split(checksum, ":")
	// default to md5 when there's no hint
	hint := "md5sum"
	if len(splittedChecksum) == 2 {
		hint = splittedChecksum[0]
	}

	checksum = splittedChecksum[len(splittedChecksum)-1]

	switch hint {
	case "md5sum":
		return validateMD5Checksum(filename, checksum)
	case "sha256":
		return validateSHA256Checksum(filename, checksum)
	default:
		return false
	}
}
