//go:build linux || darwin

package main

import (
	"io"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func consoleInfo(w io.Writer) (bool, int) {
	f, ok := w.(*os.File)
	if !ok {
		return false, 80
	}
	var size struct{ rows, cols, x, y uint16 }
	request := uintptr(0x5413)
	if runtime.GOOS == "darwin" {
		request = 0x40087468
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(unsafe.Pointer(&size)))
	if err != 0 {
		return false, 80
	}
	width := int(size.cols)
	if width < 2 {
		width = 80
	}
	return true, width
}
