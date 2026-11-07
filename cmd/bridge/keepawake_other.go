//go:build !windows

package main

// runKeepAwake is a no-op outside Windows: the Mac client does not need it,
// because a running Claude session already keeps macOS awake.
func runKeepAwake(done <-chan struct{}, stopped chan<- struct{}, started chan<- bool) {
	started <- true
	<-done
	close(stopped)
}
