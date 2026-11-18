package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

type fakeInfo struct{ dir bool }

func (f fakeInfo) Name() string       { return "" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return 0 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

func fakeNodes(names ...string) []*node {
	var nodes []*node
	for _, name := range names {
		nodes = append(nodes, &node{name: name, info: fakeInfo{}})
	}
	return nodes
}

type namespace struct {
	label string
	ok    func(rune) bool
	fits  func(string) bool
}

var namespaces = []namespace{{"udf", udfRuneOK, udfFits}, {"joliet", jolietRuneOK, jolietFits}}

// checkAssigned asserts the invariants that make "never fails" safe: every
// name is storable, unique case-insensitively, and valid names are untouched.
func checkAssigned(t *testing.T, ns namespace, nodes []*node, got map[*node]string) {
	t.Helper()
	used := map[string]string{}
	for _, n := range nodes {
		name, ok := got[n]
		if !ok {
			t.Fatalf("%s: %q got no name", ns.label, n.name)
		}
		if !validName(name, ns.ok, ns.fits) {
			t.Fatalf("%s: %q -> invalid %q", ns.label, n.name, name)
		}
		key := strings.ToUpper(name)
		if prev, dup := used[key]; dup {
			t.Fatalf("%s: %q and %q both -> %q", ns.label, prev, n.name, name)
		}
		used[key] = n.name
		if ns.label == "udf" {
			if _, err := udfEncode(name); err != nil {
				t.Fatalf("udf: %q -> %q not encodable: %v", n.name, name, err)
			}
		} else if _, err := joliet(name); err != nil {
			t.Fatalf("joliet: %q -> %q not encodable: %v", n.name, name, err)
		}
	}
}
func assign(ns namespace, nodes []*node) map[*node]string {
	got := map[*node]string{}
	assignNames(nodes, ns.ok, ns.fits, func(n *node, s string) { got[n] = s })
	return got
}

func TestNameLegalisationExpectations(t *testing.T) {
	user := "58. Different Workflows; Similar Principles.html"
	cases := []struct{ in, udf, joliet string }{
		{user, user, "58. Different Workflows_ Similar Principles.html"},
		{"a;b;c.txt", "a;b;c.txt", "a_b_c.txt"},
		{"x:y*z?.txt", "x_y_z_.txt", "x_y_z_.txt"},
		{`q"<>|\.md`, "q_____.md", "q_____.md"},
		{"tab\there", "tab_here", "tab_here"},
		{"emoji😀.txt", "emoji_.txt", "emoji_.txt"},
		{"bad\xffutf8", "bad_utf8", "bad_utf8"},
		{"trailing.", "trailing_", "trailing_"},
		{"trailing ", "trailing_", "trailing_"},
		{"CON.txt", "CON_.txt", "CON_.txt"},
		{"nul", "nul_", "nul_"},
		{"bom" + string(rune(0xfeff)), "bom_", "bom_"},
	}
	for _, c := range cases {
		for _, ns := range namespaces {
			nodes := fakeNodes(c.in)
			got := assign(ns, nodes)[nodes[0]]
			want := c.udf
			if ns.label == "joliet" {
				want = c.joliet
			}
			if got != want {
				t.Errorf("%s(%q) = %q, want %q", ns.label, c.in, got, want)
			}
		}
	}
}

func TestNameLegalisationCollisionsAndLengths(t *testing.T) {
	names := []string{
		"a:b", "a_b", "A_B.txt", "a_b.TXT", // cleaned/case collisions
		"Readme", "README", // case-only collision (possible on Linux sources)
		strings.Repeat("a", 300) + ".txt", strings.Repeat("a", 301) + ".txt", // truncation collision
		strings.Repeat("日", 200) + ".txt", strings.Repeat("😀", 100),
		"CON", "CON_", ".", "..", "", ";", ":", "...", "   ",
		".hidden", "x." + strings.Repeat("e", 40), // long extension is not kept as an extension
	}
	for _, ns := range namespaces {
		nodes := fakeNodes(names...)
		nodes[1].info = fakeInfo{dir: true}
		got := assign(ns, nodes)
		checkAssigned(t, ns, nodes, got)
		for _, n := range nodes {
			if validName(n.name, ns.ok, ns.fits) && n.name != "README" && n.name != "a_b.TXT" && got[n] != n.name {
				t.Errorf("%s: valid name %q changed to %q", ns.label, n.name, got[n])
			}
			if strings.HasSuffix(n.name, ".txt") && !strings.HasSuffix(strings.ToLower(got[n]), ".txt") {
				t.Errorf("%s: extension lost: %q -> %q", ns.label, n.name, got[n])
			}
		}
	}
}

// hostileName builds names from characters that have broken name encoders.
func hostileName(r *rand.Rand) string {
	pool := []rune{'a', 'Z', '0', '.', ' ', ';', ':', '*', '?', '"', '<', '>', '|', '\\', '/', '\t', 0, 0x7f,
		'é', 'ß', '日', '本', 0xfeff, 0xfffe, 0xffff, 0x1f600, 0x10ffff, '_', '~', '-'}
	var b strings.Builder
	for i, n := 0, r.Intn(300); i < n; i++ {
		b.WriteRune(pool[r.Intn(len(pool))])
	}
	s := b.String()
	if r.Intn(10) == 0 {
		s += "\xff\xfe" // invalid UTF-8 as a raw Unix filename can contain
	}
	return s
}
func TestNameLegalisationRandomised(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for round := 0; round < 300; round++ {
		names := make([]string, 1+r.Intn(40))
		for i := range names {
			names[i] = hostileName(r)
			if i > 0 && r.Intn(4) == 0 { // force near-duplicates
				names[i] = strings.ToUpper(names[r.Intn(i)])
			}
		}
		for _, ns := range namespaces {
			nodes := fakeNodes(names...)
			checkAssigned(t, ns, nodes, assign(ns, nodes))
		}
	}
}
func FuzzNameLegalisation(f *testing.F) {
	f.Add("58. Different Workflows; Similar Principles.html", "58. Different Workflows_ Similar Principles.html")
	f.Add("CON", "con_")
	f.Add(strings.Repeat("日", 200), "\xff")
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, ns := range namespaces {
			nodes := fakeNodes(a, b, a+b)
			checkAssigned(t, ns, nodes, assign(ns, nodes))
		}
	})
}

// --- Independent readers: parse the written image without using the writer. ---

func readSector(t *testing.T, img []byte, lba uint32) []byte {
	t.Helper()
	off := int(lba) * sector
	if off+sector > len(img) {
		t.Fatalf("sector %d outside image", lba)
	}
	return img[off : off+sector]
}

// readJoliet returns path -> content ("/" suffix marks directories).
func readJoliet(t *testing.T, img []byte) map[string][]byte {
	t.Helper()
	svd := readSector(t, img, 17)
	if svd[0] != 2 || string(svd[88:91]) != "%/E" {
		t.Fatal("no Joliet descriptor")
	}
	out := map[string][]byte{}
	var walk func(prefix string, loc, size uint32)
	walk = func(prefix string, loc, size uint32) {
		data := img[int(loc)*sector : int(loc)*sector+int(size)]
		for pos := 0; pos < len(data); {
			l := int(data[pos])
			if l == 0 {
				pos = (pos/sector + 1) * sector
				continue
			}
			rec := data[pos : pos+l]
			pos += l
			id := rec[33 : 33+int(rec[32])]
			if len(id) == 1 && id[0] <= 1 {
				continue
			}
			u := make([]uint16, len(id)/2)
			for i := range u {
				u[i] = binary.BigEndian.Uint16(id[2*i:])
			}
			name := string(utf16.Decode(u))
			eloc, esize := binary.LittleEndian.Uint32(rec[2:]), binary.LittleEndian.Uint32(rec[10:])
			if rec[25]&2 != 0 {
				out[prefix+name+"/"] = nil
				walk(prefix+name+"/", eloc, esize)
			} else {
				out[prefix+name] = append(out[prefix+name], img[int(eloc)*sector:int(eloc)*sector+int(esize)]...)
			}
		}
	}
	walk("", binary.LittleEndian.Uint32(svd[158:]), binary.LittleEndian.Uint32(svd[166:]))
	return out
}

// readUDF returns path -> content ("/" suffix marks directories).
func readUDF(t *testing.T, img []byte) map[string][]byte {
	t.Helper()
	anchor := readSector(t, img, 256)
	if le.Uint16(anchor) != 2 {
		t.Fatal("no UDF anchor")
	}
	var part, fsd uint32
	vds := le.Uint32(anchor[20:])
	for i := uint32(0); i < 16; i++ {
		b := readSector(t, img, vds+i)
		switch le.Uint16(b) {
		case 5:
			part = le.Uint32(b[188:])
		case 6:
			fsd = le.Uint32(b[252:])
		}
	}
	fileData := func(lb uint32) ([]byte, bool) {
		fe := readSector(t, img, part+lb)
		if le.Uint16(fe) != 261 {
			t.Fatalf("expected file entry at %d", lb)
		}
		size := le.Uint64(fe[56:])
		ads := fe[176+le.Uint32(fe[168:]) : 176+le.Uint32(fe[168:])+le.Uint32(fe[172:])]
		var data []byte
		for len(ads) >= 8 {
			l, loc := le.Uint32(ads), le.Uint32(ads[4:])
			ads = ads[8:]
			if l>>30 == 3 {
				aed := readSector(t, img, part+loc)
				ads = aed[24 : 24+le.Uint32(aed[20:])]
				continue
			}
			l &= 0x3fffffff
			data = append(data, img[int(part+loc)*sector:int(part+loc)*sector+int(l)]...)
		}
		if uint64(len(data)) != size {
			t.Fatalf("UDF size mismatch at %d", lb)
		}
		return data, fe[27] == 4
	}
	out := map[string][]byte{}
	var walk func(prefix string, lb uint32)
	walk = func(prefix string, lb uint32) {
		dir, _ := fileData(lb)
		for pos := 0; pos < len(dir); {
			fid := dir[pos:]
			if le.Uint16(fid) != 257 {
				t.Fatal("expected FID")
			}
			lfi, liu := int(fid[19]), int(le.Uint16(fid[36:]))
			pos += (38 + liu + lfi + 3) &^ 3
			if fid[18]&8 != 0 {
				continue
			}
			raw := fid[38+liu : 38+liu+lfi]
			var name string
			switch raw[0] {
			case 8:
				for _, c := range raw[1:] {
					name += string(rune(c))
				}
			case 16:
				u := make([]uint16, (len(raw)-1)/2)
				for i := range u {
					u[i] = binary.BigEndian.Uint16(raw[1+2*i:])
				}
				name = string(utf16.Decode(u))
			default:
				t.Fatalf("bad compression id %d", raw[0])
			}
			child := le.Uint32(fid[24:])
			data, isDir := fileData(child)
			if isDir {
				out[prefix+name+"/"] = nil
				walk(prefix+name+"/", child)
			} else {
				out[prefix+name] = data
			}
		}
	}
	fs := readSector(t, img, part+fsd)
	walk("", le.Uint32(fs[404:]))
	return out
}

// The user's failing tree, plus every other hostile name Windows can create.
func hostileTree(t *testing.T, src string) map[string]string {
	files := map[string]string{
		"Udemy - Course/5 - CCAR-F Domain 2 The Tool Layer/58. Different Workflows; Similar Principles.html": "user file",
		"Udemy - Course/1 - Intro/01. Welcome.html":                                                          "plain",
		"semi;dir;/a;b;c.txt":                   "semicolons in dir and file",
		"emoji 😀 notes.txt":                     "supplementary plane",
		strings.Repeat("L", 250) + ".txt":       "longer than UDF allows",
		strings.Repeat("日", 140) + ".txt":       "longer than UDF wide allows",
		"日本;語.txt":                              "wide with semicolon",
		"x;" + strings.Repeat("y", 70) + ".txt": "joliet long and semicolon",
		"x_" + strings.Repeat("y", 70) + ".txt": "would collide with the above in Joliet",
		"version;1":                             "looks like an ISO version",
		"empty;dir;/":                           "",
	}
	for name, body := range files {
		p := filepath.Join(src, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		put(t, p, []byte(body))
	}
	return files
}

func TestHostileNamesProduceReadableImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	files := hostileTree(t, src)
	for _, format := range []string{"bridge", "iso"} {
		image := filepath.Join(dir, format+".iso")
		report := filepath.Join(dir, format+".json")
		code, out := cli(t, dir, "create", "--source", src, "--output", image, "--format", format, "--report", report)
		if code != 0 {
			t.Fatalf("%s: %d %s", format, code, out)
		}
		img, err := os.ReadFile(image)
		if err != nil {
			t.Fatal(err)
		}
		views := map[string]map[string][]byte{"joliet": readJoliet(t, img)}
		if format == "bridge" {
			views["udf"] = readUDF(t, img)
		}
		for view, got := range views {
			var contents []string
			for p, b := range got {
				if !strings.HasSuffix(p, "/") {
					contents = append(contents, string(b))
				}
			}
			for name, body := range files {
				if strings.HasSuffix(name, "/") {
					continue
				}
				found := false
				for i, c := range contents {
					if c == body {
						found = true
						contents = append(contents[:i], contents[i+1:]...)
						break
					}
				}
				if !found {
					t.Errorf("%s/%s: content of %q missing", format, view, name)
				}
			}
			if len(contents) != 0 {
				t.Errorf("%s/%s: unexpected extra files %q", format, view, contents)
			}
		}
		if udf, ok := views["udf"]; ok {
			// Every name Windows and UDF can both hold must survive verbatim.
			for name, body := range files {
				fits := true
				for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
					fits = fits && validName(part, udfRuneOK, udfFits)
				}
				if !fits {
					continue
				}
				got, ok := udf[name]
				if !ok || string(got) != body {
					t.Errorf("UDF lost exact name %q", name)
				}
			}
		}
		var rep runReport
		b, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &rep); err != nil {
			t.Fatal(err)
		}
		if len(rep.Results) != 1 || len(rep.Results[0].Renamed) == 0 || !strings.Contains(out, "WARN renamed") || !strings.Contains(out, "renamed") {
			t.Errorf("%s: renames not reported: %s", format, out)
		}
		for _, e := range rep.Results[0].Renamed {
			if strings.Contains(e.Path, "58. Different Workflows") && format == "bridge" {
				t.Errorf("bridge renamed a UDF-valid name: %+v", e)
			}
		}
	}
}

func TestPlanNeverFailsOnNames(t *testing.T) {
	src := t.TempDir()
	hostileTree(t, src)
	for _, format := range []string{"bridge", "iso"} {
		p, err := makePlanFormat(context.Background(), src, nil, format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		var buf bytes.Buffer
		if err := writeISO(context.Background(), p, &buf, nil); err != nil {
			t.Fatal(err)
		}
		for _, n := range append(p.files, p.dirs[1:]...) {
			if !utf8.ValidString(n.jolietName) || n.jolietName == "" {
				t.Fatalf("missing Joliet name for %q", n.name)
			}
		}
	}
}
