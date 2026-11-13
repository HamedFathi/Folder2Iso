//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExclusiveCopyFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	put(t, src, []byte("image"))
	if err := copyExclusive(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	if err := copyExclusive(context.Background(), src, dst); err == nil {
		t.Fatal("overwrote existing file")
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "image" {
		t.Fatal("wrong data")
	}
	os.Remove(dst)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyExclusive(ctx, src, dst); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("partial output retained")
	}
}
