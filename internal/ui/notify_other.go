//go:build !windows

package ui

import "context"

func showSystemNotification(context.Context, string, string) error {
	return ErrNotificationsUnsupported
}
