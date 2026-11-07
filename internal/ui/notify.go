package ui

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotificationsUnsupported is returned on platforms without a toast implementation.
var ErrNotificationsUnsupported = errors.New("thông báo hệ thống chưa hỗ trợ trên nền tảng này")

// notifyMinInterval limits toasts so a burst of requests shows one notification.
const notifyMinInterval = 10 * time.Second

// Notifier shows a system notification. It must never block the caller for long.
type Notifier struct {
	show     func(ctx context.Context, title, body string) error
	mu       sync.Mutex
	lastSent time.Time
	now      func() time.Time
}

// NewNotifier returns a notifier using the platform implementation.
func NewNotifier() *Notifier {
	return &Notifier{show: showSystemNotification, now: time.Now}
}

// Notify shows a notification unless one was shown within notifyMinInterval.
// It returns false when the notification was skipped by the rate limit.
func (n *Notifier) Notify(title, body string) (bool, error) {
	if n == nil || n.show == nil {
		return false, ErrNotificationsUnsupported
	}
	n.mu.Lock()
	now := n.now()
	if !n.lastSent.IsZero() && now.Sub(n.lastSent) < notifyMinInterval {
		n.mu.Unlock()
		return false, nil
	}
	n.lastSent = now
	n.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.show(ctx, title, body); err != nil {
		return true, err
	}
	return true, nil
}
