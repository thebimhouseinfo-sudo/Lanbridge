// Package awake inhibits idle system sleep while a visible LAN Bridge process
// is running. It does not force the display on or change persistent OS policy.
package awake

import "sync"

// Guard owns one platform power assertion. Release is safe more than once.
type Guard struct {
 once sync.Once
 release func()
}

func newGuard(release func()) *Guard { return &Guard{release:release} }

func (g *Guard) Release() {
 if g==nil{return}
 g.once.Do(func(){if g.release!=nil{g.release()}})
}

// Acquire asks the OS to inhibit idle system sleep. Failure is reported so
// callers can surface an accurate degraded status without stopping Bridge.
func Acquire() (*Guard,error) {
 release,err:=acquirePlatform()
 if err!=nil{return nil,err}
 return newGuard(release),nil
}
