package main

import (
	"time"
)

// watchParentGone calls onGone once the parent process recorded at startup is
// no longer the parent (on Unix, the process is re-parented to init or a
// subreaper after its parent exits). It returns a stop function.
func watchParentGone(initialPPID int, getPPID func() int, interval time.Duration, onGone func(initial, current int)) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if current := getPPID(); current != initialPPID {
					onGone(initialPPID, current)
					return
				}
			}
		}
	}()
	var once bool
	return func() {
		if !once {
			once = true
			close(done)
		}
	}
}
