//go:build !windows

package mcpsecret

func protect([]byte)([]byte,error){return nil,ErrUnavailable}
func unprotect([]byte)([]byte,error){return nil,ErrUnavailable}
