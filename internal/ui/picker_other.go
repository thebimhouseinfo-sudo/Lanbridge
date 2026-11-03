//go:build !windows

package ui

import (
	"context"
	"errors"
)

func nativeWindowsPicker(context.Context, string) (string, error) {
	return "", errors.New("Windows native picker unavailable")
}
