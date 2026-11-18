package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgeNamesAndAliases(t *testing.T) {
	src := t.TempDir()
	long := strings.Repeat("a", 180) + ".txt"
	put(t, filepath.Join(src, long), []byte("long"))
	p, err := makePlan(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	alias := p.files[0].jolietName
	put(t, filepath.Join(src, alias), []byte("short collision"))
	put(t, filepath.Join(src, strings.Repeat("x", 254)), nil)
	put(t, filepath.Join(src, "日"+strings.Repeat("x", 126)), []byte("wide"))
	p, err = makePlan(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if p.legacyAliases != 3 {
		t.Fatal(p.legacyAliases)
	}
	used := map[string]bool{}
	for _, n := range p.files {
		if _, err := joliet(n.jolietName); err != nil {
			t.Fatal(err)
		}
		if used[n.jolietName] {
			t.Fatal("alias collision")
		}
		used[n.jolietName] = true
		if n.name == alias && n.jolietName != alias {
			t.Fatal("short name changed")
		}
	}
	strict, err := makePlanFormat(context.Background(), src, nil, "iso")
	if err != nil {
		t.Fatal("ISO format must alias long names, not fail:", err)
	}
	if strict.legacyAliases != 3 || len(strict.renamed) != 3 {
		t.Fatal("ISO format aliases", strict.legacyAliases, len(strict.renamed))
	}
	for _, name := range []string{strings.Repeat("x", 255), "日" + strings.Repeat("x", 127), "emoji😀"} {
		if _, err := udfEncode(name); err == nil {
			t.Fatal("unsupported name accepted", name)
		}
	}
}
func checkUDFTag(t *testing.T, b []byte, id uint16, loc uint32) {
	t.Helper()
	if le.Uint16(b) != id || le.Uint32(b[12:]) != loc {
		t.Fatalf("tag mismatch: %d at %d", le.Uint16(b), le.Uint32(b[12:]))
	}
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += b[i]
		}
	}
	n := int(le.Uint16(b[10:]))
	if sum != b[4] || le.Uint16(b[8:]) != udfCRC(b[16:16+n]) {
		t.Fatal("invalid tag checksum/CRC")
	}
}
func TestBridgeLayoutAndChecksums(t *testing.T) {
	src := t.TempDir()
	put(t, filepath.Join(src, "dir", "file"), []byte("test"))
	for i := 0; i < 80; i++ {
		put(t, filepath.Join(src, fmt.Sprintf("file-%03d", i)), nil)
	}
	p, err := makePlan(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeISO(context.Background(), p, &out, nil); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	for _, loc := range []uint32{256, p.blocks - 257, p.blocks - 1} {
		checkUDFTag(t, b[int(loc)*sector:], 2, loc)
	}
	for _, base := range []uint32{257, 273} {
		for i, id := range []uint16{1, 4, 5, 6, 7, 8} {
			loc := base + uint32(i)
			checkUDFTag(t, b[int(loc)*sector:], id, loc)
		}
	}
	checkUDFTag(t, b[289*sector:], 9, 289)
	checkUDFTag(t, b[int(p.udf.fsd+udfPartition)*sector:], 256, p.udf.fsd)
	for _, n := range p.udfNodes() {
		checkUDFTag(t, b[int(n.udfFE+udfPartition)*sector:], 261, n.udfFE)
	}
	for _, d := range p.dirs {
		start := uint64(d.udfDir+udfPartition) * sector
		for pos := uint64(0); pos < d.udfDirLength; {
			fid := b[start+pos:]
			checkUDFTag(t, fid, 257, d.udfDir+uint32(pos/sector))
			pos += uint64((38 + int(fid[19]) + int(binary.LittleEndian.Uint16(fid[36:])) + 3) &^ 3)
		}
	}
	if got := le.Uint16(b[int(p.root.udfFE+udfPartition)*sector+48:]); got != 2 {
		t.Fatal("directory link count", got)
	}
}
func TestUDFIndependent(t *testing.T) {
	if os.Getenv("FOLDER2ISO_UDF_TEST") != "1" {
		t.Skip("set FOLDER2ISO_UDF_TEST=1 with Python and libudfread installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	for _, name := range []string{strings.Repeat("a", 254), "日" + strings.Repeat("x", 126), strings.Repeat("d", 160) + "/file", "empty"} {
		put(t, filepath.Join(src, name), []byte("original "+name))
	}
	for i := 0; i < 100; i++ {
		put(t, filepath.Join(src, fmt.Sprintf("f-%03d", i)), []byte{byte(i)})
	}
	os.Mkdir(filepath.Join(src, "empty-dir"), 0700)
	image := filepath.Join(dir, "data.iso")
	if err := convert(context.Background(), src, image, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("python3", "tests/udf_roundtrip.py", image, src).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}
func TestUDFAEDIndependentSparse(t *testing.T) {
	if os.Getenv("FOLDER2ISO_UDF_TEST") != "1" {
		t.Skip("optional independent sparse 700 GiB extent-chain test")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	os.Mkdir(src, 0700)
	f, err := os.Create(filepath.Join(src, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	size := int64(700)*1024*1024*1024 + 123
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	offsets := []int64{0, int64(udfMaxExtent) - 2, int64(udfMaxExtent)*233 - 2, int64(udfMaxExtent)*485 - 2, size - 8}
	for _, off := range offsets {
		if _, err := f.WriteAt([]byte("MARKER"), off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	p, err := makePlan(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files[0].udfAED) != 2 {
		t.Fatal("wrong chain length", len(p.files[0].udfAED))
	}
	image := filepath.Join(dir, "sparse.iso")
	out, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.onFile = func(string, int, int) { cancel() }
	if err := writeISO(ctx, p, out, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := out.Truncate(int64(p.blocks) * sector); err != nil {
		t.Fatal(err)
	}
	for _, off := range offsets {
		if _, err := out.WriteAt([]byte("MARKER"), int64(p.files[0].data)*sector+off); err != nil {
			t.Fatal(err)
		}
	}
	for _, loc := range []uint32{p.blocks - 257, p.blocks - 1} {
		if _, err := out.WriteAt(p.udfAnchor(loc), int64(loc)*sector); err != nil {
			t.Fatal(err)
		}
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("python3", "tests/udf_roundtrip.py", image, src, "--sample-only").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
}

func TestBridgeCompatibilityExtraction(t *testing.T) {
	engine, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("xorriso unavailable")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	put(t, filepath.Join(src, strings.Repeat("d", 140), strings.Repeat("f", 180)+".txt"), []byte("long-path-data"))
	p, err := makePlan(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "image.iso")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeISO(context.Background(), p, f, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, view := range []string{"norock", "ecma119"} {
		dest := filepath.Join(dir, view)
		if out, err := exec.Command(engine, "-read_fs", view, "-osirrox", "on", "-indev", image, "-extract", "/", dest).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", view, err, out)
		}
		var contents [][]byte
		err := filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.Chmod(path, 0700)
			}
			b, e := os.ReadFile(path)
			contents = append(contents, b)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(contents) != 1 || string(contents[0]) != "long-path-data" {
			t.Fatal(view, "data mismatch")
		}
		if view == "norock" {
			path := filepath.Join(dest, p.dirs[1].jolietName, p.files[0].jolietName)
			if _, err := os.Stat(path); err != nil {
				t.Fatal("alias path", err)
			}
		}
	}
}
func TestFormatSelection(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	put(t, filepath.Join(src, "file.txt"), []byte("data"))
	for _, format := range []string{"iso", "bridge"} {
		image := filepath.Join(dir, format+".iso")
		if code, out := cli(t, dir, "create", "--source", src, "--output", image, "--format", format); code != 0 {
			t.Fatal(out)
		}
		d, err := readImageInfo(image)
		if err != nil || !d.Joliet || d.UDF != (format == "bridge") {
			t.Fatal(format, d, err)
		}
	}
	if code, _ := cli(t, dir, "create", "--source", src, "--format", "bad"); code != 2 {
		t.Fatal("invalid format accepted")
	}
}
