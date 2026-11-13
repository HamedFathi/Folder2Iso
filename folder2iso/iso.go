package main

// Native ISO 9660 level 3 / Joliet encoder. No external engines.
import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const sector = 2048
const maxExtent = uint64(0xfffff800)
const maxBlocks = uint64(0xffffffff)

type node struct {
	path, name, alias string
	jolietName        string
	udfName           []byte
	udfFE, udfDir     uint32
	udfDirLength      uint64
	udfAED            []uint32
	udfID             uint64
	info              os.FileInfo
	parent            *node
	children          []*node
	index             uint16
	loc, length       [2]uint32
	data              uint32
}
type imagePlan struct {
	udf                   *udfPlan
	legacyAliases         int
	root                  *node
	dirs, files           []*node
	pathSize              [2]uint32
	pathLoc               [2][2]uint32
	blocks                uint32
	bytes                 uint64
	label                 string
	bufferBytes, excluded int
	onFile                func(string, int, int)
}

func blocks(n uint64) uint64 { return (n + sector - 1) / sector }
func both16(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b, v)
	binary.BigEndian.PutUint16(b[2:], v)
}
func both32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
	binary.BigEndian.PutUint32(b[4:], v)
}
func joliet(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, errors.New("filename is not valid UTF-8")
	}
	if utf8.RuneCountInString(s) > 64 {
		return nil, errors.New("Joliet names are limited to 64 characters")
	}
	var b []byte
	for _, r := range s {
		if r > 0xffff || r < 32 || strings.ContainsRune("*/:;?\\", r) {
			return nil, fmt.Errorf("character %q is not supported by Joliet", r)
		}
		b = append(b, byte(r>>8), byte(r))
	}
	return b, nil
}
func identifier(n *node, v int) []byte {
	if n.parent == n {
		return []byte{0}
	}
	if v == 0 {
		return []byte(n.alias)
	}
	name := n.jolietName
	if name == "" {
		name = n.name
	}
	b, _ := joliet(name)
	return b
}
func excludedPath(relative string, patterns []string) bool {
	for _, pattern := range patterns {
		candidate := relative
		if !strings.Contains(pattern, "/") {
			candidate = path.Base(relative)
		}
		if ok, _ := path.Match(pattern, candidate); ok {
			return true
		}
	}
	return false
}
func makePlan(ctx context.Context, source string) (*imagePlan, error) {
	return makePlanOptions(ctx, source, nil)
}
func makePlanOptions(ctx context.Context, source string, excludes []string) (*imagePlan, error) {
	return makePlanFormat(ctx, source, excludes, "bridge")
}
func makePlanFormat(ctx context.Context, source string, excludes []string, format string) (*imagePlan, error) {
	if format == "" {
		format = "bridge"
	}
	if format != "bridge" && format != "iso" {
		return nil, errors.New("format must be bridge or iso")
	}
	inf, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !inf.IsDir() || inf.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("source must be a real directory")
	}
	root := &node{path: source, info: inf, index: 1}
	root.parent = root
	p := &imagePlan{root: root, dirs: []*node{root}}
	if format == "bridge" {
		p.udf = &udfPlan{}
	}
	var serial uint64
	for i := 0; i < len(p.dirs); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := p.dirs[i]
		entries, err := os.ReadDir(d.path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			file := filepath.Join(d.path, e.Name())
			rel, _ := filepath.Rel(source, file)
			if excludedPath(filepath.ToSlash(rel), excludes) {
				p.excluded++
				continue
			}
			info, err := os.Lstat(file)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return nil, fmt.Errorf("unsupported symlink/special file: %s", file)
			}
			validate := joliet
			if p.udf != nil {
				validate = udfEncode
			}
			if _, err := validate(e.Name()); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			serial++
			n := &node{path: file, name: e.Name(), info: info, parent: d, udfID: serial + 15}
			if p.udf != nil {
				n.udfName, _ = udfEncode(n.name)
			}
			if info.IsDir() {
				if len(p.dirs) >= 65535 {
					return nil, errors.New("ISO limit: at most 65,535 directories including root")
				}
				n.alias = fmt.Sprintf("D%08X", serial)
				n.index = uint16(len(p.dirs) + 1)
				p.dirs = append(p.dirs, n)
			} else {
				n.alias = fmt.Sprintf("F%08X.BIN;1", serial)
				p.files = append(p.files, n)
				p.bytes += uint64(info.Size())
				if p.bytes > maxBlocks*sector {
					return nil, errors.New("image exceeds ISO address space (approximately 8 TiB)")
				}
			}
			d.children = append(d.children, n)
		}
	}
	next := uint64(19)
	if p.udf != nil {
		p.assignJolietAliases()
		next = udfPartition
	}
	for v := 0; v < 2; v++ {
		var size uint64
		for _, d := range p.dirs {
			l := len(identifier(d, v))
			size += uint64(8 + l + l%2)
		}
		if size > 0xffffffff {
			return nil, errors.New("path table too large")
		}
		p.pathSize[v] = uint32(size)
		for e := 0; e < 2; e++ {
			p.pathLoc[v][e] = uint32(next)
			next += blocks(size)
		}
	}
	for v := 0; v < 2; v++ {
		for _, d := range p.dirs {
			size := directorySize(d, v)
			if size > 0xffffffff || next+blocks(size) > maxBlocks {
				return nil, errors.New("ISO directory/address limit exceeded")
			}
			d.loc[v] = uint32(next)
			d.length[v] = uint32(size)
			next += blocks(size)
		}
	}
	if p.udf != nil {
		next = p.planUDF(next)
	}
	for _, f := range p.files {
		if next+blocks(uint64(f.info.Size())) > maxBlocks {
			return nil, errors.New("image exceeds ISO address space")
		}
		f.data = uint32(next)
		next += blocks(uint64(f.info.Size()))
	}
	if p.udf != nil {
		p.udf.partitionLength = uint32(next + 1 - udfPartition)
		next += 257
	}
	if next+1 > maxBlocks {
		return nil, errors.New("image exceeds ISO address space")
	}
	p.blocks = uint32(next + 1)
	return p, nil
}
func recordLength(id []byte) int { return 33 + len(id) + (1 - len(id)%2) }
func sections(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	return (n + maxExtent - 1) / maxExtent
}
func sortedChildren(d *node, v int) []*node {
	c := append([]*node(nil), d.children...)
	sort.Slice(c, func(i, j int) bool { return bytes.Compare(identifier(c[i], v), identifier(c[j], v)) < 0 })
	return c
}
func directorySize(d *node, v int) uint64 {
	pos := uint64(68)
	for _, n := range sortedChildren(d, v) {
		count := uint64(1)
		if !n.info.IsDir() {
			count = sections(uint64(n.info.Size()))
		}
		l := uint64(recordLength(identifier(n, v)))
		for i := uint64(0); i < count; i++ {
			if pos%sector+l > sector {
				pos = blocks(pos) * sector
			}
			pos += l
		}
	}
	return blocks(pos) * sector
}
func record(id []byte, loc, size uint32, flags byte, date time.Time) []byte {
	b := make([]byte, recordLength(id))
	b[0] = byte(len(b))
	both32(b[2:], loc)
	both32(b[10:], size)
	t := date.UTC()
	y := t.Year()
	if y < 1900 {
		y = 1900
	}
	if y > 2155 {
		y = 2155
	}
	copy(b[18:], []byte{byte(y - 1900), byte(t.Month()), byte(t.Day()), byte(t.Hour()), byte(t.Minute()), byte(t.Second()), 0})
	b[25] = flags
	both16(b[28:], 1)
	b[32] = byte(len(id))
	copy(b[33:], id)
	return b
}
func pathTable(p *imagePlan, v, endian int) []byte {
	b := make([]byte, blocks(uint64(p.pathSize[v]))*sector)
	pos := 0
	var order binary.ByteOrder = binary.LittleEndian
	if endian == 1 {
		order = binary.BigEndian
	}
	for _, d := range p.dirs {
		id := identifier(d, v)
		b[pos] = byte(len(id))
		order.PutUint32(b[pos+2:], d.loc[v])
		order.PutUint16(b[pos+6:], d.parent.index)
		copy(b[pos+8:], id)
		pos += 8 + len(id) + len(id)%2
	}
	return b
}
func descriptor(p *imagePlan, v int) []byte {
	b := make([]byte, sector)
	b[0] = 1
	if v == 1 {
		b[0] = 2
	}
	copy(b[1:], "CD001")
	b[6] = 1
	field := func(off, n int, s string) {
		if v == 0 {
			for i := off; i < off+n; i++ {
				b[i] = ' '
			}
			copy(b[off:off+n], s)
		} else {
			for i := off; i+1 < off+n; i += 2 {
				b[i+1] = ' '
			}
			enc, _ := joliet(s)
			copy(b[off:off+n], enc)
		}
	}
	field(8, 32, "FOLDER2ISO")
	label := p.label
	if label == "" {
		label = "FOLDER2ISO"
	}
	field(40, 32, label)
	both32(b[80:], p.blocks)
	if v == 1 {
		copy(b[88:], "%/E")
	}
	both16(b[120:], 1)
	both16(b[124:], 1)
	both16(b[128:], sector)
	both32(b[132:], p.pathSize[v])
	binary.LittleEndian.PutUint32(b[140:], p.pathLoc[v][0])
	binary.BigEndian.PutUint32(b[148:], p.pathLoc[v][1])
	copy(b[156:], record([]byte{0}, p.root.loc[v], p.root.length[v], 2, p.root.info.ModTime()))
	for _, f := range [][2]int{{190, 128}, {318, 128}, {446, 128}, {574, 128}, {702, 37}, {739, 37}, {776, 37}} {
		field(f[0], f[1], "")
	}
	stamp := time.Now().UTC().Format("20060102150405") + "00"
	for _, off := range []int{813, 830, 864} {
		copy(b[off:], stamp)
	}
	copy(b[847:], "0000000000000000")
	b[881] = 1
	return b
}

type streamWriter struct {
	ctx         context.Context
	out         io.Writer
	done, total uint64
	report      func(uint64, uint64)
	last        time.Time
}

func (s *streamWriter) Write(b []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := s.out.Write(b)
	s.done += uint64(n)
	if s.report != nil && time.Since(s.last) >= 250*time.Millisecond {
		s.report(s.done, s.total)
		s.last = time.Now()
	}
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}
func zeros(w io.Writer, n uint64) error {
	var b [sector]byte
	for n > 0 {
		l := uint64(len(b))
		if n < l {
			l = n
		}
		if _, err := w.Write(b[:l]); err != nil {
			return err
		}
		n -= l
	}
	return nil
}
func writeDirectory(w io.Writer, d *node, v int) error {
	pos := uint64(0)
	emit := func(r []byte) error {
		if pos%sector+uint64(len(r)) > sector {
			pad := sector - pos%sector
			if err := zeros(w, pad); err != nil {
				return err
			}
			pos += pad
		}
		_, err := w.Write(r)
		pos += uint64(len(r))
		return err
	}
	if err := emit(record([]byte{0}, d.loc[v], d.length[v], 2, d.info.ModTime())); err != nil {
		return err
	}
	parent := d.parent
	if err := emit(record([]byte{1}, parent.loc[v], parent.length[v], 2, parent.info.ModTime())); err != nil {
		return err
	}
	for _, n := range sortedChildren(d, v) {
		id := identifier(n, v)
		if n.info.IsDir() {
			if err := emit(record(id, n.loc[v], n.length[v], 2, n.info.ModTime())); err != nil {
				return err
			}
			continue
		}
		remaining := uint64(n.info.Size())
		loc := uint64(n.data)
		for {
			size := remaining
			flag := byte(0)
			if size > maxExtent {
				size = maxExtent
				flag = 128
			}
			if err := emit(record(id, uint32(loc), uint32(size), flag, n.info.ModTime())); err != nil {
				return err
			}
			remaining -= size
			loc += blocks(size)
			if remaining == 0 {
				break
			}
		}
	}
	return zeros(w, uint64(d.length[v])-pos)
}
func unchanged(n *node) error {
	now, err := os.Lstat(n.path)
	if err != nil {
		return err
	}
	if !os.SameFile(n.info, now) || now.Size() != n.info.Size() || !now.ModTime().Equal(n.info.ModTime()) || now.Mode() != n.info.Mode() {
		return fmt.Errorf("source changed during conversion: %s", n.path)
	}
	return nil
}
func writeISO(ctx context.Context, p *imagePlan, out io.Writer, report func(uint64, uint64)) error {
	w := &streamWriter{ctx: ctx, out: out, total: uint64(p.blocks) * sector, report: report}
	if err := zeros(w, 16*sector); err != nil {
		return err
	}
	for v := 0; v < 2; v++ {
		if _, err := w.Write(descriptor(p, v)); err != nil {
			return err
		}
	}
	end := make([]byte, sector)
	end[0] = 255
	copy(end[1:], "CD001")
	end[6] = 1
	if _, err := w.Write(end); err != nil {
		return err
	}
	if p.udf != nil {
		if err := p.writeUDFHeader(w); err != nil {
			return err
		}
	}
	for v := 0; v < 2; v++ {
		for e := 0; e < 2; e++ {
			if _, err := w.Write(pathTable(p, v, e)); err != nil {
				return err
			}
		}
	}
	for v := 0; v < 2; v++ {
		for _, d := range p.dirs {
			if err := writeDirectory(w, d, v); err != nil {
				return err
			}
		}
	}
	if p.udf != nil {
		if err := p.writeUDFMetadata(w); err != nil {
			return err
		}
	}
	bufsize := p.bufferBytes
	if bufsize == 0 {
		bufsize = 4 * 1024 * 1024
	}
	buffer := make([]byte, bufsize)
	for index, n := range p.files {
		if p.onFile != nil {
			rel, _ := filepath.Rel(p.root.path, n.path)
			p.onFile(rel, index+1, len(p.files))
		}
		if err := unchanged(n); err != nil {
			return err
		}
		f, err := os.Open(n.path)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !os.SameFile(info, n.info) {
			f.Close()
			return fmt.Errorf("source replaced: %s", n.path)
		}
		copied, copyErr := io.CopyBuffer(w, io.LimitReader(f, n.info.Size()), buffer)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("%s: %w", n.path, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if copied != n.info.Size() {
			return fmt.Errorf("source shortened: %s", n.path)
		}
		if err := unchanged(n); err != nil {
			return err
		}
		if err := zeros(w, blocks(uint64(copied))*sector-uint64(copied)); err != nil {
			return err
		}
	}
	for _, d := range p.dirs {
		if err := unchanged(d); err != nil {
			return err
		}
	}
	if err := zeros(w, sector); err != nil {
		return err
	}
	if p.udf != nil {
		if _, err := w.Write(p.udfAnchor(uint32(w.done / sector))); err != nil {
			return err
		}
		if err := zeros(w, 255*sector); err != nil {
			return err
		}
		if _, err := w.Write(p.udfAnchor(uint32(w.done / sector))); err != nil {
			return err
		}
	}
	if w.done != w.total {
		return errors.New("internal ISO layout mismatch")
	}
	if report != nil {
		report(w.done, w.total)
	}
	return nil
}
