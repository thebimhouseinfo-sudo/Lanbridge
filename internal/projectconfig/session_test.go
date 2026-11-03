package projectconfig

import (
 "errors"
 "testing"
)

func TestSessionRequiresBothLoadedAndMatchingID(t *testing.T) {
 var s Session
 if err:=s.Require("a");!errors.Is(err,ErrNotOpen){t.Fatal(err)}
 if err:=s.SetWindowsLoaded("a");err!=nil{t.Fatal(err)}
 if s.Snapshot().Ready{t.Fatal("Windows-only must not be ready")}
 if err:=s.SetMacLoaded("b");err!=nil{t.Fatal(err)}
 if err:=s.Require("a");!errors.Is(err,ErrMismatch){t.Fatalf("mismatch %v",err)}
 if err:=s.SetMacLoaded("a");err!=nil{t.Fatal(err)}
 if err:=s.Require("a");err!=nil{t.Fatal(err)}
 if err:=s.Require("b");!errors.Is(err,ErrMismatch){t.Fatal(err)}
 s.Clear()
 if err:=s.Require("a");!errors.Is(err,ErrNotOpen){t.Fatal("reconnect must clear readiness")}
}
