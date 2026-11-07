package main

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchParentGoneFiresWhenParentChanges(t *testing.T) {
	var ppid atomic.Int64
	ppid.Store(4242)
	fired := make(chan [2]int, 1)
	stop := watchParentGone(4242, func() int { return int(ppid.Load()) }, 5*time.Millisecond, func(initial, current int) {
		fired <- [2]int{initial, current}
	})
	defer stop()

	ppid.Store(1)
	select {
	case got := <-fired:
		if got != [2]int{4242, 1} {
			t.Fatalf("onGone args = %v, want [4242 1]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onGone did not fire after parent changed")
	}
}

func TestWatchParentGoneStaysQuietWhileParentAlive(t *testing.T) {
	fired := make(chan struct{}, 1)
	stop := watchParentGone(7, func() int { return 7 }, 5*time.Millisecond, func(int, int) {
		fired <- struct{}{}
	})
	select {
	case <-fired:
		t.Fatal("onGone fired while parent was unchanged")
	case <-time.After(50 * time.Millisecond):
	}
	stop()
	stop()
}

func TestIsOrphanedAtStart(t *testing.T) {
	if !isOrphanedAtStart(1) && runtime.GOOS != "windows" {
		t.Fatal("ppid 1 must count as orphaned on Unix")
	}
	if isOrphanedAtStart(4242) {
		t.Fatal("a live parent pid must not count as orphaned")
	}
}
