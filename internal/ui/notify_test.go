package ui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNotifierRateLimitsBursts(t *testing.T) {
	var shown []string
	clock := time.Unix(1000, 0)
	n := &Notifier{
		show: func(_ context.Context, title, _ string) error { shown = append(shown, title); return nil },
		now:  func() time.Time { return clock },
	}
	if sent, err := n.Notify("first", "a"); !sent || err != nil {
		t.Fatalf("first notification sent=%v err=%v", sent, err)
	}
	if sent, _ := n.Notify("burst", "b"); sent {
		t.Fatal("second notification inside the interval must be skipped")
	}
	clock = clock.Add(notifyMinInterval + time.Second)
	if sent, _ := n.Notify("later", "c"); !sent {
		t.Fatal("notification after the interval must be sent")
	}
	if len(shown) != 2 || shown[0] != "first" || shown[1] != "later" {
		t.Fatalf("shown = %v", shown)
	}
}

func TestNotifierReportsShowFailure(t *testing.T) {
	n := &Notifier{
		show: func(context.Context, string, string) error { return errors.New("no toast host") },
		now:  time.Now,
	}
	sent, err := n.Notify("t", "b")
	if !sent || err == nil {
		t.Fatalf("failure must be reported: sent=%v err=%v", sent, err)
	}
}
