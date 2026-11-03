//go:build !darwin && !windows

package awake

import "errors"
func acquirePlatform()(func(),error){
 return nil,errors.New("idle sleep inhibition unavailable on this platform")
}
