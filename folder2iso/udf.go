package main

// Read-only UDF 1.02 bridge. All file payload extents are shared with ISO/Joliet.
// Structures follow ECMA-167 (3rd edition) and OSTA UDF revision 1.02.
import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

const udfPartition = 320
const udfMaxExtent = uint64(0x3ffff800)

type udfPlan struct{ fsd, partitionLength uint32 }

var le = binary.LittleEndian

func udfEncode(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("filename is not valid UTF-8")
	}
	wide := false
	for _, r := range s {
		if !udfRuneOK(r) {
			return nil, fmt.Errorf("character %q is not supported by this UDF 1.02 bridge", r)
		}
		if r > 255 {
			wide = true
		}
	}
	b := []byte{8}
	if wide {
		b[0] = 16
	}
	for _, r := range s {
		if wide {
			b = append(b, byte(r>>8))
		}
		b = append(b, byte(r))
	}
	if len(b) > 255 {
		return nil, fmt.Errorf("UDF name exceeds 255 encoded bytes (254 Latin-1 or 127 other BMP characters)")
	}
	return b, nil
}
func (p *imagePlan) udfNodes() []*node {
	n := append([]*node(nil), p.dirs...)
	return append(n, p.files...)
}
func udfChunks(size uint64) int { return int((size + udfMaxExtent - 1) / udfMaxExtent) }
func udfAEDCount(count int) int {
	if count <= 234 {
		return 0
	}
	count -= 233
	result := 1
	for count > 253 {
		result++
		count -= 252
	}
	return result
}
func (p *imagePlan) planUDF(next uint64) uint64 {
	p.udf.fsd = uint32(next - udfPartition)
	next += 2
	for _, d := range p.dirs {
		d.udfDirLength = 40
		for _, n := range d.children {
			d.udfDirLength += uint64((38 + len(n.udfName) + 3) &^ 3)
		}
	}
	for _, n := range p.udfNodes() {
		n.udfFE = uint32(next - udfPartition)
		next++
		size := uint64(n.info.Size())
		if n.info.IsDir() {
			size = n.udfDirLength
		}
		for i := 0; i < udfAEDCount(udfChunks(size)); i++ {
			n.udfAED = append(n.udfAED, uint32(next-udfPartition))
			next++
		}
	}
	for _, d := range p.dirs {
		d.udfDir = uint32(next - udfPartition)
		next += blocks(d.udfDirLength)
	}
	return next
}
func udfCRC(b []byte) uint16 {
	var c uint16
	for _, v := range b {
		c ^= uint16(v) << 8
		for i := 0; i < 8; i++ {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}
func udfTag(b []byte, id uint16, loc uint32) []byte {
	le.PutUint16(b, id)
	le.PutUint16(b[2:], 2)
	le.PutUint16(b[6:], 1)
	le.PutUint16(b[8:], udfCRC(b[16:]))
	le.PutUint16(b[10:], uint16(len(b)-16))
	le.PutUint32(b[12:], loc)
	b[4] = 0
	for i := 0; i < 16; i++ {
		if i != 4 {
			b[4] += b[i]
		}
	}
	return b
}
func udfSector(b []byte) []byte { out := make([]byte, sector); copy(out, b); return out }
func udfReg(b []byte, name string, revision bool) {
	copy(b[1:24], name)
	if revision {
		le.PutUint16(b[24:], 0x0102)
	}
	if name == "*OSTA UDF Compliant" {
		b[26] = 1
	}
}
func udfCharset(b []byte) { copy(b[1:64], "OSTA Compressed Unicode") }
func udfString(b []byte, s string) {
	enc, _ := udfEncode(s)
	if len(enc) > len(b)-1 {
		enc = enc[:len(b)-1]
	}
	copy(b, enc)
	b[len(b)-1] = byte(len(enc))
}
func udfTime(b []byte, t time.Time) {
	t = t.UTC()
	le.PutUint16(b, 0x1000)
	le.PutUint16(b[2:], uint16(t.Year()))
	copy(b[4:], []byte{byte(t.Month()), byte(t.Day()), byte(t.Hour()), byte(t.Minute()), byte(t.Second()), 0, 0, 0})
}
func udfLong(b []byte, size, loc uint32) { le.PutUint32(b, size); le.PutUint32(b[4:], loc) }
func (p *imagePlan) udfLabel() string {
	if p.label == "" {
		return "FOLDER2ISO"
	}
	return p.label
}
func (p *imagePlan) udfAnchor(loc uint32) []byte {
	b := make([]byte, 512)
	le.PutUint32(b[16:], 16*sector)
	le.PutUint32(b[20:], 257)
	le.PutUint32(b[24:], 16*sector)
	le.PutUint32(b[28:], 273)
	return udfSector(udfTag(b, 2, loc))
}
func (p *imagePlan) udfVolume(kind int, loc uint32) []byte {
	b := make([]byte, 512)
	le.PutUint32(b[16:], uint32(kind+1))
	id := uint16(0)
	switch kind {
	case 0:
		id = 1
		udfString(b[24:56], p.udfLabel())
		le.PutUint16(b[56:], 1)
		le.PutUint16(b[58:], 1)
		le.PutUint16(b[60:], 2)
		le.PutUint16(b[62:], 3)
		le.PutUint32(b[64:], 1)
		le.PutUint32(b[68:], 1)
		udfString(b[72:200], fmt.Sprintf("%016x_FOLDER2ISO", uint64(p.root.info.ModTime().UnixNano())))
		udfCharset(b[200:264])
		udfCharset(b[264:328])
		udfReg(b[344:376], "*folder2iso", false)
		udfTime(b[376:388], p.root.info.ModTime())
		udfReg(b[388:420], "*folder2iso", false)
	case 1:
		id = 4
		udfReg(b[20:52], "*UDF LV Info", true)
		udfCharset(b[52:116])
		udfString(b[116:244], p.udfLabel())
		udfReg(b[352:384], "*folder2iso", false)
	case 2:
		id = 5
		le.PutUint16(b[20:], 1)
		udfReg(b[24:56], "+NSR02", false)
		le.PutUint32(b[184:], 1)
		le.PutUint32(b[188:], udfPartition)
		le.PutUint32(b[192:], p.udf.partitionLength)
		udfReg(b[196:228], "*folder2iso", false)
	case 3:
		id = 6
		b = b[:446]
		udfCharset(b[20:84])
		udfString(b[84:212], p.udfLabel())
		le.PutUint32(b[212:], sector)
		udfReg(b[216:248], "*OSTA UDF Compliant", true)
		udfLong(b[248:264], 2*sector, p.udf.fsd)
		le.PutUint32(b[264:], 6)
		le.PutUint32(b[268:], 1)
		udfReg(b[272:304], "*folder2iso", false)
		le.PutUint32(b[432:], 16*sector)
		le.PutUint32(b[436:], 289)
		b[440] = 1
		b[441] = 6
		le.PutUint16(b[442:], 1)
	case 4:
		id = 7
		b = b[:24]
	case 5:
		id = 8
		clear(b[16:])
	}
	return udfSector(udfTag(b, id, loc))
}
func (p *imagePlan) udfIntegrity() []byte {
	b := make([]byte, 134)
	udfTime(b[16:28], p.root.info.ModTime())
	le.PutUint32(b[28:], 1)
	le.PutUint64(b[40:], uint64(len(p.dirs)+len(p.files)+16))
	le.PutUint32(b[72:], 1)
	le.PutUint32(b[76:], 46)
	le.PutUint32(b[84:], p.udf.partitionLength)
	udfReg(b[88:120], "*folder2iso", false)
	le.PutUint32(b[120:], uint32(len(p.files)))
	le.PutUint32(b[124:], uint32(len(p.dirs)))
	for _, off := range []int{128, 130, 132} {
		le.PutUint16(b[off:], 0x0102)
	}
	return udfSector(udfTag(b, 9, 289))
}
func (p *imagePlan) writeUDFHeader(w io.Writer) error {
	for _, name := range []string{"BEA01", "NSR02", "TEA01"} {
		b := make([]byte, sector)
		copy(b[1:], name)
		b[6] = 1
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zeros(w, (256-22)*sector); err != nil {
		return err
	}
	if _, err := w.Write(p.udfAnchor(256)); err != nil {
		return err
	}
	for _, base := range []uint32{257, 273} {
		for k := 0; k < 16; k++ {
			b := make([]byte, sector)
			if k < 6 {
				b = p.udfVolume(k, base+uint32(k))
			}
			if _, err := w.Write(b); err != nil {
				return err
			}
		}
	}
	if _, err := w.Write(p.udfIntegrity()); err != nil {
		return err
	}
	if _, err := w.Write(udfSector(udfTag(make([]byte, 512), 8, 290))); err != nil {
		return err
	}
	return zeros(w, (udfPartition-291)*sector)
}
func (p *imagePlan) udfFileSet() []byte {
	b := make([]byte, 512)
	udfTime(b[16:28], p.root.info.ModTime())
	le.PutUint16(b[28:], 3)
	le.PutUint16(b[30:], 3)
	le.PutUint32(b[32:], 1)
	le.PutUint32(b[36:], 1)
	udfCharset(b[48:112])
	udfString(b[112:240], p.udfLabel())
	udfCharset(b[240:304])
	udfString(b[304:336], p.udfLabel())
	udfLong(b[400:416], sector, p.root.udfFE)
	udfReg(b[416:448], "*OSTA UDF Compliant", true)
	return udfSector(udfTag(b, 256, p.udf.fsd))
}

type udfAllocation struct{ size, loc uint32 }

func udfAllocations(n *node) []udfAllocation {
	size := uint64(n.info.Size())
	loc := n.data - udfPartition
	if n.info.IsDir() {
		size = n.udfDirLength
		loc = n.udfDir
	}
	var ads []udfAllocation
	for size > 0 {
		l := size
		if l > udfMaxExtent {
			l = udfMaxExtent
		}
		ads = append(ads, udfAllocation{uint32(l), loc})
		size -= l
		loc += uint32(blocks(l))
	}
	return ads
}
func udfPutAds(b []byte, ads []udfAllocation) {
	for i, a := range ads {
		le.PutUint32(b[i*8:], a.size)
		le.PutUint32(b[i*8+4:], a.loc)
	}
}
func udfFE(n *node, ads []udfAllocation) []byte {
	b := make([]byte, 176+8*len(ads))
	le.PutUint16(b[20:], 4)
	le.PutUint16(b[24:], 1)
	b[27] = 5
	// Strategy 4 has no previously recorded ICB. Keep ParentICBLocation zero.
	le.PutUint32(b[36:], 0xffffffff)
	le.PutUint32(b[40:], 0xffffffff)
	le.PutUint32(b[44:], 0x1084)
	le.PutUint16(b[48:], 1)
	size := uint64(n.info.Size())
	if n.info.IsDir() {
		b[27] = 4
		size = n.udfDirLength
		links := uint16(1)
		for _, c := range n.children {
			if c.info.IsDir() {
				links++
			}
		}
		le.PutUint16(b[48:], links)
		le.PutUint32(b[44:], 0x14a5)
	}
	le.PutUint64(b[56:], size)
	le.PutUint64(b[64:], blocks(size))
	for _, off := range []int{72, 84, 96} {
		udfTime(b[off:off+12], n.info.ModTime())
	}
	le.PutUint32(b[108:], 1)
	udfReg(b[128:160], "*folder2iso", false)
	le.PutUint64(b[160:], n.udfID)
	le.PutUint32(b[172:], uint32(len(ads)*8))
	udfPutAds(b[176:], ads)
	return udfSector(udfTag(b, 261, n.udfFE))
}
func writeUDFEntry(w io.Writer, n *node) error {
	ads := udfAllocations(n)
	take := len(ads)
	if len(n.udfAED) > 0 {
		take = 233
	}
	first := append([]udfAllocation(nil), ads[:take]...)
	ads = ads[take:]
	if len(n.udfAED) > 0 {
		first = append(first, udfAllocation{0xc0000000 | sector, n.udfAED[0]})
	}
	if _, err := w.Write(udfFE(n, first)); err != nil {
		return err
	}
	for i, loc := range n.udfAED {
		take = len(ads)
		if i+1 < len(n.udfAED) {
			take = 252
		}
		current := append([]udfAllocation(nil), ads[:take]...)
		ads = ads[take:]
		if i+1 < len(n.udfAED) {
			current = append(current, udfAllocation{0xc0000000 | sector, n.udfAED[i+1]})
		}
		b := make([]byte, 24+len(current)*8)
		le.PutUint32(b[20:], uint32(len(current)*8))
		udfPutAds(b[24:], current)
		if _, err := w.Write(udfSector(udfTag(b, 258, loc))); err != nil {
			return err
		}
	}
	return nil
}
func udfFID(n *node, parent bool, loc uint32) []byte {
	name := n.udfName
	if parent {
		name = nil
	}
	b := make([]byte, (38+len(name)+3)&^3)
	le.PutUint16(b[16:], 1)
	if n.info.IsDir() {
		b[18] = 2
	}
	if parent {
		b[18] |= 8
	}
	b[19] = byte(len(name))
	udfLong(b[20:36], sector, n.udfFE)
	copy(b[38:], name)
	return udfTag(b, 257, loc)
}
func (p *imagePlan) writeUDFMetadata(w io.Writer) error {
	if _, err := w.Write(p.udfFileSet()); err != nil {
		return err
	}
	if _, err := w.Write(udfSector(udfTag(make([]byte, 512), 8, p.udf.fsd+1))); err != nil {
		return err
	}
	for _, n := range p.udfNodes() {
		if err := writeUDFEntry(w, n); err != nil {
			return err
		}
	}
	for _, d := range p.dirs {
		pos := uint64(0)
		emit := func(n *node, parent bool) error {
			b := udfFID(n, parent, d.udfDir+uint32(pos/sector))
			_, err := w.Write(b)
			pos += uint64(len(b))
			return err
		}
		if err := emit(d.parent, true); err != nil {
			return err
		}
		for _, n := range d.children {
			if err := emit(n, false); err != nil {
				return err
			}
		}
		if err := zeros(w, blocks(pos)*sector-pos); err != nil {
			return err
		}
	}
	return nil
}
