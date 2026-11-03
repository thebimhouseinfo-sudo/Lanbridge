//go:build windows

package mcpsecret

import (
 "errors"
 "runtime"
 "syscall"
 "unsafe"
)

type dataBlob struct{size uint32;bytes *byte}
var crypt32=syscall.NewLazyDLL("crypt32.dll")
var kernel32=syscall.NewLazyDLL("kernel32.dll")
var protectData=crypt32.NewProc("CryptProtectData")
var unprotectData=crypt32.NewProc("CryptUnprotectData")
var localFree=kernel32.NewProc("LocalFree")
const cryptProtectUIForbidden=0x1

func crypt(blob []byte,protecting bool)([]byte,error){
 if len(blob)==0{return nil,errors.New("empty MCP secret material")}
 input:=dataBlob{size:uint32(len(blob)),bytes:&blob[0]}
 output:=dataBlob{}
 proc:=protectData
 if !protecting{proc=unprotectData}
 ret,_,callErr:=proc.Call(uintptr(unsafe.Pointer(&input)),0,0,0,0,cryptProtectUIForbidden,uintptr(unsafe.Pointer(&output)))
 runtime.KeepAlive(blob)
 if ret==0{return nil,callErr}
 defer localFree.Call(uintptr(unsafe.Pointer(output.bytes)))
 if output.bytes==nil||output.size==0{return nil,errors.New("DPAPI returned no data")}
 result:=make([]byte,output.size)
 copy(result,unsafe.Slice(output.bytes,int(output.size)))
 return result,nil
}
func protect(data []byte)([]byte,error){return crypt(data,true)}
func unprotect(data []byte)([]byte,error){return crypt(data,false)}
