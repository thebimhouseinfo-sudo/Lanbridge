package main

import (
	"log/slog"
	"sync"
	"time"
)

// hostKeepAwakeRefresh is how often the awake request is renewed. Windows keeps
// the state only while the owning thread keeps it set, so it is re-asserted.
const hostKeepAwakeRefresh = 60 * time.Second

// startHostKeepAwake keeps the Windows host from sleeping while it runs. The
// display may still turn off. The returned function releases the request and
// is safe to call more than once.
func startHostKeepAwake() func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	started := make(chan bool, 1)
	go runKeepAwake(done, stopped, started)
	if ok := <-started; !ok {
		slog.Warn("Could not keep the computer awake; it may sleep and drop the bridge connection")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}
