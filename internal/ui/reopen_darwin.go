//go:build darwin

package ui

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
void lanbridgeInstallReopen(void);
*/
import "C"

import "sync/atomic"

var reopenCallback atomic.Value // func()

//export lanbridgeReopen
func lanbridgeReopen() {
	if f, ok := reopenCallback.Load().(func()); ok && f != nil {
		go f()
	}
}

// InstallReopenHandler makes a click on the running app (Finder, Dock,
// Spotlight) call onReopen, since LSUIElement apps otherwise ignore it.
func InstallReopenHandler(onReopen func()) {
	reopenCallback.Store(onReopen)
	C.lanbridgeInstallReopen()
}
