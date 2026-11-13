package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type result struct {
	LegacyAliases int       `json:"legacy_name_aliases"`
	Source        string    `json:"source"`
	Output        string    `json:"output"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	Files         int       `json:"files"`
	Directories   int       `json:"directories"`
	Excluded      int       `json:"excluded_entries"`
	SourceBytes   uint64    `json:"source_bytes"`
	ImageBytes    uint64    `json:"image_bytes"`
	SHA256        string    `json:"sha256,omitempty"`
	Started       time.Time `json:"started"`
	Duration      float64   `json:"duration_seconds"`
	Scan          float64   `json:"scan_seconds"`
	Write         float64   `json:"write_seconds"`
	Flush         float64   `json:"flush_seconds"`
	Verify        float64   `json:"verify_seconds"`
	Publish       float64   `json:"publish_seconds"`
}

func convert(ctx context.Context, source, target string, out io.Writer) error {
	o := options{progress: "none", interval: time.Second, bufferMiB: 4, label: "FOLDER2ISO", existing: "error"}
	_, err := runJob(ctx, job{source: source, target: target}, o, &outputSink{out: out}, "[1/1]")
	return err
}
func runJob(ctx context.Context, j job, o options, sink *outputSink, prefix string) (r result, err error) {
	r = result{Source: j.source, Output: j.target, Status: "failed", Started: time.Now()}
	defer func() {
		r.Duration = time.Since(r.Started).Seconds()
		if err != nil {
			r.Error = err.Error()
			if ctx.Err() != nil {
				r.Status = "cancelled"
			}
		}
	}()
	title := prefix + " " + shortName(filepath.Base(j.target))
	sink.progress(title+" | scanning", o.progress)
	sink.detail("%s Source: %s\n%s Output: %s", prefix, j.source, prefix, j.target)
	tick := time.Now()
	p, err := makePlanFormat(ctx, j.source, o.excludes, o.format)
	r.Scan = time.Since(tick).Seconds()
	if err != nil {
		return r, err
	}
	p.label = o.label
	p.bufferBytes = o.bufferMiB * 1024 * 1024
	r.Files = len(p.files)
	r.Directories = len(p.dirs)
	r.Excluded = p.excluded
	r.LegacyAliases = p.legacyAliases
	r.SourceBytes = p.bytes
	r.ImageBytes = uint64(p.blocks) * sector
	sink.detail("%s %d files | %d directories | %d exclusions | source %s | image %s", prefix, r.Files, r.Directories, r.Excluded, humanBytes(r.SourceBytes), humanBytes(r.ImageBytes))
	if o.dry {
		r.Status = "planned"
		return r, nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(j.target), ".folder2iso-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(stage)
	temp := filepath.Join(stage, "image.partial")
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return r, err
	}
	var digest hash.Hash
	var writer io.Writer = f
	if o.verify || o.checksum {
		digest = sha256.New()
		writer = io.MultiWriter(f, digest)
	}
	view := progressView{sink: sink, prefix: title, mode: o.progress, started: time.Now(), interval: o.interval}
	p.onFile = func(name string, index, total int) {
		if o.verbose {
			sink.detail("%s FILE %d/%d %s", prefix, index, total, name)
		}
	}
	tick = time.Now()
	err = writeISO(ctx, p, writer, view.report)
	r.Write = time.Since(tick).Seconds()
	if err == nil {
		sink.progress(title+" | flushing", o.progress)
		tick = time.Now()
		err = f.Sync()
		r.Flush = time.Since(tick).Seconds()
	}
	closeErr := f.Close()
	if err != nil {
		return r, err
	}
	if closeErr != nil {
		return r, closeErr
	}
	if digest != nil {
		r.SHA256 = hex.EncodeToString(digest.Sum(nil))
	}
	if o.verify {
		tick = time.Now()
		view = progressView{sink: sink, prefix: prefix + " verify " + shortName(filepath.Base(j.target)), mode: o.progress, started: time.Now(), interval: o.interval}
		actual, hashErr := hashFile(ctx, temp, view.report)
		r.Verify = time.Since(tick).Seconds()
		if hashErr != nil {
			return r, hashErr
		}
		if actual != r.SHA256 {
			return r, fmt.Errorf("readback verification failed")
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	sink.progress(title+" | publishing", o.progress)
	tick = time.Now()
	if o.existing == "replace" {
		err = replaceImage(ctx, temp, j.target)
	} else {
		err = publish(ctx, temp, j.target, sinkWriter{sink: sink, prefix: prefix})
	}
	r.Publish = time.Since(tick).Seconds()
	if err != nil {
		return r, err
	}
	r.Status = "created"
	return r, nil
}

type sinkWriter struct {
	sink   *outputSink
	prefix string
}

func (w sinkWriter) Write(b []byte) (int, error) {
	w.sink.detail("%s %s", w.prefix, strings.TrimSpace(string(b)))
	return len(b), nil
}
func hashFile(ctx context.Context, path string, report func(uint64, uint64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	inf, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !inf.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	h := sha256.New()
	w := &streamWriter{ctx: ctx, out: h, total: uint64(inf.Size()), report: report}
	if _, err := io.CopyBuffer(w, io.LimitReader(f, inf.Size()), make([]byte, 4*1024*1024)); err != nil {
		return "", err
	}
	now, err := f.Stat()
	if err != nil {
		return "", err
	}
	if w.done != uint64(inf.Size()) || now.Size() != inf.Size() || !now.ModTime().Equal(inf.ModTime()) {
		return "", fmt.Errorf("image changed while hashing")
	}
	if report != nil {
		report(w.done, w.total)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
