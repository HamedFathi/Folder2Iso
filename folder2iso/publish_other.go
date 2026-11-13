//go:build !windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func publish(ctx context.Context, source, target string, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Link(source, target); err == nil {
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return err
	}
	fmt.Fprintln(out, "Final copy required on this filesystem.")
	return copyExclusive(ctx, source, target)
}
func copyExclusive(ctx context.Context, source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(target)
		}
	}()
	w := &streamWriter{ctx: ctx, out: out}
	if _, err := io.CopyBuffer(w, in, make([]byte, 4*1024*1024)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
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
	return os.Rename(source, target)
}
