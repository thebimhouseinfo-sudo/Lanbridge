package main

import "testing"

func TestStartHostKeepAwakeStopIsIdempotent(t *testing.T) {
	stop := startHostKeepAwake()
	stop()
	stop()
}
