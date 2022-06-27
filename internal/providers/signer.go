package providers

import (
	"os"

	"github.com/frankbraun/gosignify/signify"
	"github.com/pkg/errors"
)

var (
	ErrSignerKeyFile    = errors.New("open key file error")
	ErrSignerPubKey     = errors.New("signer requires a public key parameter")
	ErrSignerPrivateKey = errors.New("signer requires a private key parameter")
	ErrSignerSign       = errors.New("error signing file")
	ErrSignerVerify     = errors.New("error verifying file signature")
)

const (
	SigSuffix = ".sig"
)

// Signer exposes methods to sign and verify files
type Signer struct {
	// path to the private key file - required only for signing
	privateKeyFile string
	// path to the public key
	publicKeyFile string
}

// NewSigner returns a Signer object to sign and verify files
//
// A signer always requires the public key, the private key is optional
// since its required only to sign files.
func NewSigner(privateKeyFile, publicKeyfile string) (*Signer, error) {
	if publicKeyfile == "" {
		return nil, ErrSignerPubKey
	}

	// expect atleast one of the keys present, and check they can be accessed
	for _, f := range []string{privateKeyFile, publicKeyfile} {
		if f == "" {
			continue
		}

		_, err := os.Open(f)
		if err != nil {
			return nil, errors.Wrap(err, ErrSignerKeyFile.Error())
		}
	}

	return &Signer{privateKeyFile: privateKeyFile, publicKeyFile: publicKeyfile}, nil
}

// Sign signs the targetFile and stores the signature in the sigFile
//
// sigFile is optional and defaults to targetFile.sig
func (s *Signer) Sign(targetFile, sigFile string) error {
	if s.privateKeyFile == "" {
		return ErrSignerPrivateKey
	}

	args := []string{"gosignify", "-S", "-m", targetFile, "-s", s.privateKeyFile, "-p", s.publicKeyFile, "-q"}
	if sigFile != "" {
		args = append(args, []string{"-x", sigFile}...)
	}

	err := signify.Main(args...)
	if err != nil {
		return errors.Wrap(err, ErrSignerSign.Error())
	}

	return nil
}

// Verify verifies signature on the targetFile
//
// sigFile is optional and defaults to targetFile.sig
func (s *Signer) Verify(targetFile, sigFile string) error {
	if s.publicKeyFile == "" {
		return ErrSignerPubKey
	}

	args := []string{"gosignify", "-V", "-m", targetFile, "-p", s.publicKeyFile, "-q"}
	if sigFile != "" {
		args = append(args, []string{"-x", sigFile}...)
	}

	err := signify.Main(args...)
	if err != nil {
		return errors.Wrap(err, ErrSignerVerify.Error())
	}

	return nil
}
