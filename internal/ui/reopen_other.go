//go:build !darwin

package ui

// InstallReopenHandler is a no-op outside macOS.
func InstallReopenHandler(func()) {}
