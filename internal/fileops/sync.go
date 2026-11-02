package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
)

// SyncItem represents a file to be synced
type SyncItem struct {
	RelPath string `json:"rel_path"`
	Data    []byte `json:"data"`
}

// SyncResult represents the sync outcome for a file
type SyncResult struct {
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256"`
	Status  string `json:"status"` // "ok" or error string
}

// SyncFiles atomically writes a batch of files to the target directory after verifying permissions and limits
func SyncFiles(gate *permissions.Gatekeeper, targetBaseDir string, files []SyncItem, maxSyncMB int) ([]SyncResult, error) {
	if maxSyncMB <= 0 {
		maxSyncMB = 5
	}
	maxBytes := int64(maxSyncMB) * 1024 * 1024

	// 1. Pre-check total size
	var totalBytes int64
	for _, f := range files {
		totalBytes += int64(len(f.Data))
	}
	if totalBytes > maxBytes {
		return nil, fmt.Errorf("ERR_TOO_LARGE: total sync payload %d bytes exceeds %d MB limit", totalBytes, maxSyncMB)
	}

	results := make([]SyncResult, 0, len(files))

	// 2. Perform atomic sync for each file
	for _, file := range files {
		if file.RelPath == "" || filepath.Clean(file.RelPath) == "." {
			return nil, errors.New("ERR_PARAMS: file path must name a file")
		}
		resolvedPath, err := permissions.Resolve(targetBaseDir, file.RelPath)
		if err != nil {
			return nil, fmt.Errorf("ERR_PERMISSION_DENIED: %w", err)
		}

		resolvedPath, err = gate.ResolveAccess(resolvedPath, permissions.CapSync)
		if err != nil {
			return nil, fmt.Errorf("%w for file %s", err, file.RelPath)
		}

		parentDir := filepath.Dir(resolvedPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return nil, fmt.Errorf("ERR_IO: failed to create parent directory: %w", err)
		}

		// Check if target file exists and is locked (e.g. DLL loaded by AutoCAD)
		if _, err := os.Stat(resolvedPath); err == nil {
			testLock, err := os.OpenFile(resolvedPath, os.O_WRONLY, 0644)
			if err != nil {
				if isFileLockError(err) {
					return nil, fmt.Errorf("ERR_FILE_LOCKED: file %q is locked by another process (e.g. AutoCAD)", file.RelPath)
				}
				return nil, fmt.Errorf("ERR_IO: file access error: %w", err)
			}
			if err := testLock.Close(); err != nil {
				return nil, fmt.Errorf("ERR_IO: failed to close existing target: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("ERR_IO: failed to inspect target file: %w", err)
		}

		// A unique temp file avoids following a pre-existing .tmp symlink.
		temp, err := os.CreateTemp(parentDir, ".lanbridge-sync-*")
		if err != nil {
			return nil, fmt.Errorf("ERR_IO: failed to create temp file: %w", err)
		}
		tempFile := temp.Name()
		if _, err := temp.Write(file.Data); err != nil {
			_ = temp.Close()
			return nil, fmt.Errorf("ERR_IO: failed to write temp file: %w", err)
		}
		if err := temp.Close(); err != nil {
			_ = os.Remove(tempFile)
			return nil, fmt.Errorf("ERR_IO: failed to close temp file: %w", err)
		}

		// Atomic replace via rename
		if err := os.Rename(tempFile, resolvedPath); err != nil {
			_ = os.Remove(tempFile)
			if isFileLockError(err) {
				return nil, fmt.Errorf("ERR_FILE_LOCKED: file %q is locked by another process (e.g. AutoCAD)", file.RelPath)
			}
			return nil, fmt.Errorf("ERR_IO: atomic rename failed: %w", err)
		}

		// Calculate SHA-256
		hash := sha256.Sum256(file.Data)
		hashStr := hex.EncodeToString(hash[:])

		results = append(results, SyncResult{
			RelPath: file.RelPath,
			SHA256:  hashStr,
			Status:  "ok",
		})
	}

	return results, nil
}
