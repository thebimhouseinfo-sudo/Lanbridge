package fileops

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

const (
	MaxReadChunkSize = 1024 * 1024 // 1 MB per chunk
)

// FileItem represents metadata for an entry in a directory
type FileItem struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ListDir lists entries in a directory after gate check
func ListDir(gate *permissions.Gatekeeper, dirPath string) ([]FileItem, error) {
	resolvedDir, err := gate.ResolveAccess(dirPath, permissions.CapRead)
	if err != nil {return nil,err}
	if err:=denyBridgePrivatePath(resolvedDir);err!=nil{return nil,err}
	entries, err := os.ReadDir(resolvedDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("ERR_NOT_FOUND: directory %s does not exist", resolvedDir)
		}
		return nil, fmt.Errorf("ERR_IO: %w", err)
	}

	res := make([]FileItem, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("ERR_IO: inspect entry %s: %w", e.Name(), err)
		}
		res = append(res, FileItem{
			Name:    e.Name(),
			Path:    filepath.Join(resolvedDir, e.Name()),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return res, nil
}

// GetFileInfo retrieves metadata for a single file after gate check
func GetFileInfo(gate *permissions.Gatekeeper, filePath string) (*FileItem, error) {
	resolvedPath, err := gate.ResolveAccess(filePath, permissions.CapRead)
	if err != nil {return nil,err}
	if err:=denyBridgePrivatePath(resolvedPath);err!=nil{return nil,err}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("ERR_NOT_FOUND: file %s does not exist", resolvedPath)
		}
		return nil, fmt.Errorf("ERR_IO: %w", err)
	}

	return &FileItem{
		Name:    info.Name(),
		Path:    resolvedPath,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// ReadFileChunk reads a chunk of a file up to MaxReadChunkSize (1MB)
func ReadFileChunk(gate *permissions.Gatekeeper, filePath string, offset int64, length int) ([]byte, bool, error) {
	resolvedPath, err := gate.ResolveAccess(filePath, permissions.CapRead)
	if err != nil {return nil,false,err}
	if err:=denyBridgePrivatePath(resolvedPath);err!=nil{return nil,false,err}
	if offset < 0 {
		return nil, false, errors.New("ERR_PARAMS: offset cannot be negative")
	}

	if length < 0 {
		return nil, false, errors.New("ERR_PARAMS: length cannot be negative")
	}
	if length == 0 || length > MaxReadChunkSize {
		length = MaxReadChunkSize
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, fmt.Errorf("ERR_NOT_FOUND: %w", err)
		}
		return nil, false, fmt.Errorf("ERR_IO: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("ERR_IO: %w", err)
	}

	if offset >= stat.Size() {
		return []byte{}, true, nil // EOF
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("ERR_IO: seek failed: %w", err)
	}

	buf := make([]byte, length)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("ERR_IO: read failed: %w", err)
	}

	isEOF := (offset + int64(n)) >= stat.Size()
	return buf[:n], isEOF, nil
}

// ReadTailLines reads the last N lines of a file (useful for inspecting log crashes)
func ReadTailLines(gate *permissions.Gatekeeper, filePath string, numLines int) ([]string, error) {
	resolvedPath, err := gate.ResolveAccess(filePath, permissions.CapRead)
	if err != nil {return nil,err}
	if err:=denyBridgePrivatePath(resolvedPath);err!=nil{return nil,err}
	if numLines < 0 {
		return nil, errors.New("ERR_PARAMS: tail_lines cannot be negative")
	}
	if numLines == 0 {
		return []string{}, nil
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("ERR_NOT_FOUND: file %s does not exist", resolvedPath)
		}
		return nil, fmt.Errorf("ERR_IO: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxReadChunkSize+1)
	var retainedBytes int
	for scanner.Scan() {
		line := scanner.Text()
		lineBytes := len(line) + 1
		lines = append(lines, line)
		retainedBytes += lineBytes
		if len(lines) > numLines {
			retainedBytes -= len(lines[0]) + 1
			lines = lines[1:]
		}
		if retainedBytes > MaxReadChunkSize {
			return nil, fmt.Errorf("ERR_LIMIT: tail_lines result exceeds %d bytes", MaxReadChunkSize)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ERR_IO: scan failed: %w", err)
	}
	return lines, nil
}

func denyBridgePrivatePath(path string) error {
 dir,err:=store.GetConfigDir()
 if err!=nil{return err}
 if projectconfig.IsBridgePrivatePath(path,dir) {
  return fmt.Errorf("ERR_CONFIG_PROTECTED: Bridge App Data cannot be sent through generic File Read/Sync")
 }
 return nil
}
