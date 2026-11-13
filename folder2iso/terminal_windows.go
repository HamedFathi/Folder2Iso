//go:build windows

package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

var consoleDLL = syscall.NewLazyDLL("kernel32.dll")
var getConsoleMode = consoleDLL.NewProc("GetConsoleMode")
var getConsoleBufferInfo = consoleDLL.NewProc("GetConsoleScreenBufferInfo")

func consoleInfo(w io.Writer) (bool, int) {
	f, ok := w.(*os.File)
	if !ok {
		return false, 80
	}
	var mode uint32
	okCall, _, _ := getConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	if okCall == 0 {
		return false, 80
	}
	var b struct {
		sizeX, sizeY, cursorX, cursorY       int16
		attributes                           uint16
		left, top, right, bottom, maxX, maxY int16
	}
	okCall, _, _ = getConsoleBufferInfo.Call(f.Fd(), uintptr(unsafe.Pointer(&b)))
	if okCall != 0 {
		width := int(b.right - b.left + 1)
		if width > 1 {
			return true, width
		}
	}
	return true, 80
}
