package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func cli(t *testing.T, cwd string, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := runAt(context.Background(), args, &out, cwd)
	return code, out.String()
}
func TestWorkingDirectoryDefault(t *testing.T) {
	// run(), not runAt(), must obtain the shell directory; the test executable lives elsewhere.
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	put(t, filepath.Join(dir, "Photos", "file.txt"), []byte("hello"))
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	var out bytes.Buffer
	if code := run(context.Background(), nil, &out); code != 0 {
		t.Fatalf("%d: %s", code, &out)
	}
	if _, err := os.Stat(filepath.Join(dir, "Photos.iso")); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 || strings.Contains(out.String(), "Press Enter") {
		t.Fatal(out.String())
	}
}
func TestRelativePathsAndCommands(t *testing.T) {
	old, _ := os.Getwd()
	dir := t.TempDir()
	put(t, filepath.Join(dir, "Photos", "file.txt"), []byte("hello"))
	os.Chdir(dir)
	defer os.Chdir(old)
	code, out := cli(t, dir, "create", "--source", "Photos", "--output-dir", "images", "--name", "Vacation", "--verify", "--report", "run.json", "--log", "run.log")
	if code != 0 {
		t.Fatalf("%d: %s", code, out)
	}
	if strings.Count(out, "\n") != 1 || strings.Contains(out, "Scanning") || strings.Contains(out, "SHA256") {
		t.Fatal(out)
	}
	b, err := os.ReadFile("run.json")
	if err != nil {
		t.Fatal(err)
	}
	var r runReport
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Created != 1 || !validHash(r.Results[0].SHA256) {
		t.Fatalf("%+v", r)
	}
	for _, args := range [][]string{{"info", "--image", "images/Vacation.iso"}, {"verify", "--image", "images/Vacation.iso", "--sha256", r.Results[0].SHA256}} {
		code, out = cli(t, dir, args...)
		if code != 0 || strings.Count(out, "\n") != 1 || strings.Contains(out, "\r") {
			t.Fatalf("%d: %s", code, out)
		}
	}
	code, out = cli(t, dir, "verify", "--image", "images/Vacation.iso", "--sha256", r.Results[0].SHA256, "--json")
	if code != 0 || !json.Valid([]byte(out)) {
		t.Fatal(out)
	}
	log, _ := os.ReadFile("run.log")
	if bytes.Contains(log, []byte("\r")) || bytes.Contains(log, []byte("ETA")) {
		t.Fatal("progress leaked into log")
	}
}
func TestParallelSingleLine(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"One", "Two", "Three"} {
		put(t, filepath.Join(dir, n, "file"), bytes.Repeat([]byte("x"), 65536))
	}
	code, out := cli(t, dir, "batch", "--source", dir, "--jobs", "3", "--progress", "line", "--verify")
	if code != 0 {
		t.Fatal(out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("scrolling output: %q", out)
	}
	if !strings.Contains(out, "\r") || !strings.Contains(out, "3 created") {
		t.Fatal(out)
	}
	for _, frame := range strings.Split(out, "\r") {
		if strings.Contains(frame, "\n") {
			continue
		}
		width := 0
		for _, r := range frame {
			width += cellWidth(r)
		}
		if width > 79 {
			t.Fatalf("wrap: %d %q", width, frame)
		}
	}
	code, out = cli(t, dir, "batch", "--jobs", "2")
	if code != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, "3 skipped") {
		t.Fatalf("%d: %s", code, out)
	}
}
func TestProgressClipsAndClears(t *testing.T) {
	var out bytes.Buffer
	s := &outputSink{out: &out, widthOverride: 32}
	s.progress(strings.Repeat("日", 80), "bar")
	s.progress("short", "bar")
	s.line("Done")
	text := out.String()
	if strings.Count(text, "\n") != 1 {
		t.Fatal(text)
	}
	for _, frame := range strings.Split(text, "\r") {
		width := 0
		for _, r := range strings.TrimSuffix(frame, "\n") {
			width += cellWidth(r)
		}
		if width > 31 {
			t.Fatal("wide frame", width)
		}
	}
	if !strings.Contains(text, "short                         ") {
		t.Fatal("old progress not erased", text)
	}
}
func TestAutoProgressNonTerminal(t *testing.T) {
	for _, w := range []io.Writer{&bytes.Buffer{}, io.Discard} {
		mode, err := resolveProgress("auto", w)
		if err != nil || mode != "none" {
			t.Fatal(mode, err)
		}
	}
}
func TestVerboseOptIn(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "source", "file"), []byte("x"))
	code, out := cli(t, dir, "create", "--source", filepath.Join(dir, "source"), "--verbose")
	if code != 0 || !strings.Contains(out, "FILE 1/1") || !strings.Contains(out, "duration") {
		t.Fatal(out)
	}
}
func TestSafetyAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	put(t, filepath.Join(src, "file"), []byte("original"))
	image := filepath.Join(dir, "custom.iso")
	code, out := cli(t, dir, "create", "--source", src, "--output", image)
	if code != 0 {
		t.Fatal(out)
	}
	old, _ := os.ReadFile(image)
	put(t, filepath.Join(src, strings.Repeat("x", 127)+"日"), []byte("invalid"))
	code, _ = cli(t, dir, "create", "--source", src, "--output", image, "--overwrite")
	now, _ := os.ReadFile(image)
	if code != 1 || !bytes.Equal(old, now) {
		t.Fatal("damaged original")
	}
	os.Remove(filepath.Join(src, strings.Repeat("x", 127)+"日"))
	put(t, filepath.Join(src, "file"), []byte("replacement"))
	code, out = cli(t, dir, "create", "--source", src, "--output", image, "--overwrite")
	if code != 0 {
		t.Fatal(out)
	}
	now, _ = os.ReadFile(image)
	if bytes.Equal(old, now) {
		t.Fatal("not replaced")
	}
	for _, extra := range [][]string{{"--output", filepath.Join(src, "self.iso")}, {"--log", filepath.Join(src, "self.log")}, {"--report", filepath.Join(src, "self.json")}} {
		args := append([]string{"create", "--source", src}, extra...)
		if code, _ := cli(t, dir, args...); code == 0 {
			t.Fatal("unsafe output accepted")
		}
	}
}
func TestFiltersDryRunAndTimeout(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "ProjectA", "keep"), []byte("a"))
	put(t, filepath.Join(dir, "ProjectA", "cache", "emoji😀"), []byte("excluded"))
	put(t, filepath.Join(dir, "Other", "emoji😀"), []byte("not selected"))
	dest := filepath.Join(dir, "images")
	args := []string{"batch", "--include-folder", "Project*", "--exclude", "cache", "--output-dir", dest, "--prefix", "b-", "--suffix", "-2026"}
	code, out := cli(t, dir, append(args, "--dry-run")...)
	if code != 0 || !strings.Contains(out, "planned") {
		t.Fatal(out)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dry run wrote output")
	}
	code, out = cli(t, dir, args...)
	if code != 0 {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(dest, "b-ProjectA-2026.iso")); err != nil {
		t.Fatal(err)
	}
	code, _ = cli(t, dir, "batch", "--timeout", "1ns")
	if code != 124 {
		t.Fatal(code)
	}
}
func TestCancellationAndSourceChange(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "file"), []byte("x"))
	p, err := makePlan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	err = writeISO(ctx, p, &out, func(_, _ uint64) { cancel() })
	if err != context.Canceled {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "file"), []byte("changed"))
	if err := writeISO(context.Background(), p, io.Discard, nil); err == nil {
		t.Fatal("source changes ignored")
	}
}
func TestIndependentExtraction(t *testing.T) {
	engine, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("independent reader unavailable")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	files := map[string][]byte{"日本語.txt": []byte("unicode"), ".hidden": []byte("hidden"), "nested/space name.txt": []byte("data\x00"), "empty": {}, strings.Repeat("a", 64): []byte("long")}
	for n, b := range files {
		put(t, filepath.Join(src, n), b)
	}
	os.Mkdir(filepath.Join(src, "empty-dir"), 0700)
	image := filepath.Join(dir, "data.iso")
	if err := convert(context.Background(), src, image, io.Discard); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "extract")
	cmd := exec.Command(engine, "-osirrox", "on", "-indev", image, "-extract", "/", dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	filepath.WalkDir(dest, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0700)
		}
		return nil
	})
	for n, want := range files {
		got, err := os.ReadFile(filepath.Join(dest, n))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal(n, err)
		}
	}
}
func TestLargeRoundTrip(t *testing.T) {
	if os.Getenv("FOLDER2ISO_LARGE_TEST") != "1" {
		t.Skip("optional >4 GiB test")
	}
	engine, err := exec.LookPath("xorriso")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	os.Mkdir(src, 0700)
	file := filepath.Join(src, "large.bin")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(1<<32) + 123
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{0, int64(maxExtent) - 2, int64(maxExtent) + 5, size - 8} {
		if _, err := f.WriteAt([]byte("MARKER"), off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	image := filepath.Join(dir, "large.iso")
	if err := convert(context.Background(), src, image, io.Discard); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FOLDER2ISO_UDF_TEST") == "1" {
		if b, err := exec.Command("python3", "tests/udf_roundtrip.py", image, src).CombinedOutput(); err != nil {
			t.Fatalf("UDF: %v %s", err, b)
		}
	}
	dest := filepath.Join(dir, "extracted")
	if b, err := exec.Command(engine, "-osirrox", "on", "-indev", image, "-extract", "/", dest).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	os.Chmod(dest, 0700)
	hash := func(p string) string {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	if hash(file) != hash(filepath.Join(dest, "large.bin")) {
		t.Fatal("large-file mismatch")
	}
}
func TestProgressCompletionNoDuplicate(t *testing.T) {
	var out bytes.Buffer
	p := progressView{sink: &outputSink{out: &out}, prefix: "test", mode: "plain", started: time.Now(), interval: time.Second}
	p.report(10, 10)
	p.report(10, 10)
	if strings.Count(out.String(), "100.0%") != 1 {
		t.Fatal(out.String())
	}
}

func TestDefaultRetryOnlyCreatesMissingImages(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"A-Good", "C-Good"} {
		put(t, filepath.Join(dir, name, "file.txt"), []byte(name))
	}
	badName := strings.Repeat("x", 127) + "日"
	badFile := filepath.Join(dir, "B-FixMe", badName)
	put(t, badFile, []byte("retry payload"))
	code, out := cli(t, dir)
	if code != 1 || !strings.Contains(out, "2 created") || !strings.Contains(out, "1 failed") {
		t.Fatalf("initial run: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "B-FixMe.iso")); !os.IsNotExist(err) {
		t.Fatal("failed conversion published an image")
	}
	type savedImage struct {
		data []byte
		info os.FileInfo
	}
	saved := map[string]savedImage{}
	for _, name := range []string{"A-Good", "C-Good"} {
		file := filepath.Join(dir, name+".iso")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		saved[file] = savedImage{data, info}
	}
	if err := os.Rename(badFile, filepath.Join(dir, "B-FixMe", "fixed.txt")); err != nil {
		t.Fatal(err)
	}
	// A finished source is deliberately no longer valid: its existing ISO must
	// bypass scanning entirely, not merely avoid publication after doing the work.
	put(t, filepath.Join(dir, "A-Good", badName), []byte("do not rescan"))
	code, out = cli(t, dir)
	if code != 0 || !strings.Contains(out, "1 created, 2 skipped") {
		t.Fatalf("retry: %d %s", code, out)
	}
	if _, err := readImageInfo(filepath.Join(dir, "B-FixMe.iso")); err != nil {
		t.Fatal(err)
	}
	for file, previous := range saved {
		now, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(now, previous.data) || !os.SameFile(previous.info, info) || !previous.info.ModTime().Equal(info.ModTime()) {
			t.Fatalf("completed image was rewritten: %s", file)
		}
	}
	code, out = cli(t, dir)
	if code != 0 || !strings.Contains(out, "0 created, 3 skipped") {
		t.Fatalf("completed rerun: %d %s", code, out)
	}
}

func TestReleaseVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		code, out := cli(t, t.TempDir(), args...)
		if code != 0 || strings.TrimSpace(out) != "1.0.0" {
			t.Fatalf("%v: %d %q", args, code, out)
		}
	}
	code, out := cli(t, t.TempDir(), "--help")
	if code != 0 || !strings.Contains(out, "folder2iso 1.0.0") {
		t.Fatalf("help: %d %s", code, out)
	}
}
