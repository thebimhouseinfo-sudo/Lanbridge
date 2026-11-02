//go:build !windows

package fileops

func isFileLockError(error) bool {
	return false
}
