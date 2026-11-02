package permissions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrAccessDenied = errors.New("access denied: path outside permitted boundary")
	ErrPathEscape   = errors.New("access denied: path traversal or escape detected")
)

// Resolve resolves a relative path under an existing permitted base directory.
// Existing symlink ancestors are resolved before the boundary check, including
// when the requested leaf does not exist yet.
func Resolve(allowedBase string, requestedRelPath string) (string, error) {
	if allowedBase == "" {
		return "", errors.New("allowed base path cannot be empty")
	}
	if filepath.IsAbs(requestedRelPath) || filepath.VolumeName(requestedRelPath) != "" {
		return "", fmt.Errorf("%w: absolute paths are not allowed", ErrPathEscape)
	}
	for _, part := range strings.FieldsFunc(requestedRelPath, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return "", fmt.Errorf("%w: parent path segments are not allowed", ErrPathEscape)
		}
	}

	base, err := filepath.Abs(filepath.Clean(allowedBase))
	if err != nil {
		return "", fmt.Errorf("resolve allowed base: %w", err)
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve allowed base %q: %w", allowedBase, err)
	}

	target := filepath.Clean(filepath.Join(base, requestedRelPath))
	if !isSubPath(base, target) {
		return "", fmt.Errorf("%w: requested path escapes allowed base", ErrPathEscape)
	}

	resolvedTarget, err := resolveExistingAncestor(target)
	if err != nil {
		return "", err
	}
	if !isSubPath(base, resolvedTarget) {
		return "", fmt.Errorf("%w: target %q is outside allowed base %q", ErrAccessDenied, resolvedTarget, base)
	}
	return resolvedTarget, nil
}

func resolveExistingAncestor(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", fmt.Errorf("resolve path %q: %w", current, evalErr)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect path %q: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor for path %q", path)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func isSubPath(base, target string) bool {
	b, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return false
	}
	t, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}

	if runtime.GOOS == "windows" {
		b = strings.ToLower(b)
		t = strings.ToLower(t)
	}
	if b == t {
		return true
	}

	rel, err := filepath.Rel(b, t)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
