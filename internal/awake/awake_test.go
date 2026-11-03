package awake

import (
 "sync"
 "testing"
)

func TestReleaseIsIdempotentAndConcurrentSafe(t *testing.T){
 calls:=0
 var mu sync.Mutex
 g:=newGuard(func(){mu.Lock();calls++;mu.Unlock()})
 var wg sync.WaitGroup
 for i:=0;i<20;i++{wg.Add(1);go func(){defer wg.Done();g.Release()}()}
 wg.Wait()
 if calls!=1{t.Fatalf("release called %d times",calls)}
}
func TestNilRelease(t *testing.T){var g *Guard;g.Release()}
func TestNativeLifecycle(t *testing.T){
 g,err:=Acquire()
 if err!=nil{t.Skipf("platform assertion unavailable on this runner: %v",err)}
 g.Release()
 g.Release()
}
