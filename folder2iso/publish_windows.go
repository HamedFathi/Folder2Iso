//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func windowsLongPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(abs, `\\?\`) {
		return abs, nil
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:], nil
	}
	return `\\?\` + abs, nil
}
func movePointers(source, target string) (*uint16, *uint16, error) {
	from, err := windowsLongPath(source)
	if err != nil {
		return nil, nil, err
	}
	to, err := windowsLongPath(target)
	if err != nil {
		return nil, nil, err
	}
	a, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return nil, nil, err
	}
	b, err := syscall.UTF16PtrFromString(to)
	return a, b, err
}
func publish(ctx context.Context, source, target string, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, b, err := movePointers(source, target)
	if err != nil {
		return err
	}
	return syscall.MoveFile(a, b)
}
func replaceImage(ctx context.Context, source, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if inf, err := os.Lstat(target); err == nil {
		if !inf.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file: %s", target)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	a, b, err := movePointers(source, target)
	if err != nil {
		return err
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if ok == 0 {
		return callErr
	}
	return nil
}
