package providers

import (
	"context"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Defines methods to sign and verify

var (
	ErrRemoteSignFail   = errors.New("error in generating checksum and signature for remote file")
	ErrDownloadSign     = errors.New("error downloading files to sign")
	ErrUploadSigned     = errors.New("error uploading signed file(s)")
	ErrDownloadVerify   = errors.New("error downloading files to verify")
	ErrRemoteVerifyFail = errors.New("error in verifying signature for remote repofiles")
)

// SignFileStoreFile signs a file present in the filestore
// srcPath: is the full path to the file in the filestore
//
// nolint:gocyclo // TODO: figure if this method can be split up
func SignFilestoreFile(ctx context.Context, srcPath, fileSHA256, tmpDir string, downloader *Downloader, signer *Signer, logger *logrus.Logger) error {
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(tmpDir, "sign-")
	if err != nil {
		return errors.Wrap(ErrRemoteSignFail, err.Error())
	}

	srcPathDir := path.Dir(srcPath)

	// base directory with random name for tmp files
	tmpDirBase := path.Base(tmpDir)
	defer os.RemoveAll(path.Join(tmpDir, tmpDirBase))

	// Download srcPath file to tmp directory - the local tmp directory is mounted with /tmp
	// hence here we pass the relative path asda/tmpFile
	tmpFileRelPath := path.Join(tmpDirBase, path.Base(srcPath))

	rootDir := downloader.FilestoreRootDir()
	if rootDir == "/" {
		rootDir = ""
	}

	srcURL := downloader.FilestoreURL() + rootDir + srcPath

	logger.WithFields(
		logrus.Fields{
			"src": srcURL,
			"tmp": tmpDir,
		},
	).Trace("download file to checksum, sign")

	err = downloader.CopyFilestoreToLocalTmp(ctx, tmpFileRelPath, srcPath)
	if err != nil {
		return errors.Wrap(ErrDownloadSign, err.Error())
	}

	// Absolute path for the tmp file to download the remote file as
	tmpFile := path.Join(tmpDir, path.Base(srcPath))

	// /tmp/asda/tmpFilePath.SHA256
	tmpChecksumFile := tmpFile + SumSuffix

	// /tmp/asda/tmpFilePath.SHA256.sig
	tmpSigFile := tmpFile + SumSuffix + SigSuffix

	// verify file checksum - when given
	if fileSHA256 != "" {
		err = SHA256ChecksumValidate(tmpFile, fileSHA256)
		if err != nil {
			return err
		}
	}

	// Generate a checksum file for tmpFile
	err = SHA256Checksum(tmpFile)
	if err != nil {
		return err
	}

	// Sign checksumFile, store the resulting signature in sigFile
	err = signer.Sign(tmpChecksumFile, tmpSigFile)
	if err != nil {
		return err
	}

	for _, file := range []string{tmpChecksumFile, tmpSigFile} {
		// upload the checksum and signature tmp files to the dst directory
		// downloader.tmp is mounted with /tmp as the root, hence this mangling (TODO: consider mounting / ?)
		tmpFileRelPath := path.Join(tmpDirBase, path.Base(file))
		// downloader.filestore need to be given the full path including the filename or we end up clobbering a directory
		dstFileRelPath := path.Join(srcPathDir, path.Base(file))

		rootDir := downloader.FilestoreRootDir()
		if rootDir == "/" {
			rootDir = ""
		}

		dstURL := downloader.FilestoreURL() + rootDir + srcPath

		logger.WithFields(
			logrus.Fields{
				"src": path.Join(tmpDir, path.Base(file)),
				"dst": dstURL,
			},
		).Trace("upload checksum and signature files")

		err = downloader.CopyLocalTmpToFilestore(ctx, dstFileRelPath, tmpFileRelPath)
		if err != nil {
			return errors.Wrap(ErrUploadSigned, err.Error())
		}
	}

	return nil
}

// VerifyUpdateURL verifies the checksum, signature of the file linked in the UpdateURL parameter
//
// returns nil if verify was successful
func VerifyUpdateURL(ctx context.Context, updateURL, filename, fileSHA256, tmpDir string, downloader *Downloader, signer *Signer, logger *logrus.Logger) error {
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(tmpDir, "verify-")
	if err != nil {
		return errors.Wrap(ErrRemoteVerifyFail, err.Error())
	}

	// base directory with random name for tmp files
	tmpDirBase := path.Base(tmpDir)
	defer os.RemoveAll(tmpDir)

	// https://foo.baz/firmware/.../filename.SHA256
	checksumURL := updateURL + SumSuffix
	// https://foo.baz/firmware/.../filename.SHA256.sig
	sigFileURL := checksumURL + SigSuffix

	// Download repo file to tmp directory - the local tmp directory is mounted with /tmp
	// hence here we pass the relative path asda/{bin, bin.SHA256, bin.SHA256.sig}
	for _, endpoint := range []string{updateURL, checksumURL, sigFileURL} {
		// download the checksum and signature tmp files to the dst directory
		// downloader.tmp is mounted with /tmp as the root, hence this mangling (TODO: consider mounting / ?)
		tmpFileRelPath := path.Join(tmpDirBase, path.Base(endpoint))

		logger.WithFields(
			logrus.Fields{
				"tmp": path.Join(tmpDir, path.Base(endpoint)),
				"src": endpoint,
			}).Trace("download file to verify checksum, signature")

		err = downloader.CopyURLToLocalTmp(ctx, tmpFileRelPath, endpoint)
		if err != nil {
			return errors.Wrap(ErrDownloadVerify, err.Error())
		}
	}

	// absolute path to the sig, checksum file
	tmpFilename := path.Join(tmpDir, filename)
	tmpChecksumFile := path.Join(tmpDir, filepath.Base(checksumURL))
	tmpSigFile := path.Join(tmpDir, filepath.Base(sigFileURL))

	// verify signature of checksum file
	err = signer.Verify(tmpChecksumFile, tmpSigFile)
	if err != nil {
		return err
	}

	// verify bin file checksum
	err = SHA256ChecksumValidate(tmpFilename, fileSHA256)
	if err != nil {
		return err
	}

	return nil
}
