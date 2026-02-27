// Package transfer provides SFTP-based file push and pull operations.
package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// PushFile uploads a local file to a remote path on a single host via SFTP.
// It computes a SHA-256 checksum during transfer and verifies it remotely.
func PushFile(ctx context.Context, sshClient *ssh.Client, localPath, remotePath, host string, progressFn ProgressFunc) (checksum string, bytesWritten int64, err error) {
	localFile, err := os.Open(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("open local file: %w", err)
	}
	defer localFile.Close()

	stat, err := localFile.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat local file: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return "", 0, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	// Ensure remote directory exists. Use path (not filepath) because
	// remotePath is always a Unix path on the remote host.
	remoteDir := path.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if err := sftpClient.MkdirAll(remoteDir); err != nil {
			return "", 0, fmt.Errorf("create remote dir %s: %w", remoteDir, err)
		}
	}

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return "", 0, fmt.Errorf("create remote file: %w", err)
	}

	hasher := sha256.New()
	pw := newProgressWriter(remoteFile, host, stat.Size(), progressFn)
	writer := io.MultiWriter(pw, hasher)

	written, err := copyWithContext(ctx, writer, localFile)
	// Close the remote file to flush writes before checksum verification.
	remoteFile.Close()
	if err != nil {
		return "", written, fmt.Errorf("copy: %w", err)
	}

	localChecksum := hex.EncodeToString(hasher.Sum(nil))

	// Verify checksum by re-reading the remote file on the same SFTP session.
	remoteChecksum, err := remoteSHA256(sftpClient, remotePath)
	if err != nil {
		return localChecksum, written, fmt.Errorf("remote checksum verification failed: %w", err)
	}
	if remoteChecksum != localChecksum {
		return localChecksum, written, fmt.Errorf("checksum mismatch: local=%s remote=%s", localChecksum, remoteChecksum)
	}

	return localChecksum, written, nil
}

// PullFile downloads a remote file to a local directory via SFTP.
// Files are saved as localDir/<host>/<filename>.
func PullFile(ctx context.Context, sshClient *ssh.Client, remotePath, localDir, host string, progressFn ProgressFunc) (checksum string, bytesWritten int64, err error) {
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return "", 0, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return "", 0, fmt.Errorf("open remote file: %w", err)
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat remote file: %w", err)
	}

	// Create local directory: localDir/<host>/
	hostDir := filepath.Join(localDir, host)
	if err := os.MkdirAll(hostDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create local dir: %w", err)
	}

	localPath := filepath.Join(hostDir, filepath.Base(remotePath))
	localFile, err := os.Create(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("create local file: %w", err)
	}
	defer localFile.Close()

	hasher := sha256.New()
	pw := newProgressWriter(localFile, host, stat.Size(), progressFn)
	writer := io.MultiWriter(pw, hasher)

	written, err := copyWithContext(ctx, writer, remoteFile)
	if err != nil {
		return "", written, fmt.Errorf("copy: %w", err)
	}

	localChecksum := hex.EncodeToString(hasher.Sum(nil))

	// Verify checksum by re-reading the remote file on the same SFTP session.
	remoteChecksum, err := remoteSHA256(sftpClient, remotePath)
	if err != nil {
		return localChecksum, written, fmt.Errorf("remote checksum verification failed: %w", err)
	}
	if remoteChecksum != localChecksum {
		return localChecksum, written, fmt.Errorf("checksum mismatch: local=%s remote=%s", localChecksum, remoteChecksum)
	}

	return localChecksum, written, nil
}

// remoteSHA256 computes the SHA-256 checksum of a remote file by reading it
// back over SFTP. This avoids shell command injection risks and doesn't
// require sha256sum to be installed on the remote host.
func remoteSHA256viasftp(sftpClient *sftp.Client, remotePath string) (string, error) {
	f, err := sftpClient.Open(remotePath)
	if err != nil {
		return "", fmt.Errorf("open remote file for checksum: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("read remote file for checksum: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

var remoteSHA256 = remoteSHA256viasftp

// copyWithContext copies from src to dst, checking for context cancellation
// periodically via a buffered copy.
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, readErr := src.Read(buf)
		if nr > 0 {
			nw, writeErr := dst.Write(buf[:nr])
			written += int64(nw)
			if writeErr != nil {
				return written, writeErr
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}
