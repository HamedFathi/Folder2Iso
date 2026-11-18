package main

// On-disc name assignment. Every source entry receives a name that each
// namespace (UDF, Joliet) can store and that Windows can open. Names that are
// already valid are kept verbatim; anything else is rewritten deterministically
// instead of failing the whole image.
import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Characters Windows cannot use in a filename. ';' is deliberately absent:
// Windows and UDF allow it, only Joliet forbids it.
const windowsReservedChars = `"*/:<>?\|`

// udfRuneOK reports whether r can be stored in a UDF 1.02 (OSTA CS0, UCS-2)
// name that every mainstream reader, including Windows, can open.
func udfRuneOK(r rune) bool {
	return r >= 32 && r <= 0xffff && r != 0xfeff && r < 0xfffe &&
		(r < 0xd800 || r > 0xdfff) && !strings.ContainsRune(windowsReservedChars, r)
}

// jolietRuneOK adds the Joliet-specific ban on ';' (the ISO version separator).
func jolietRuneOK(r rune) bool { return udfRuneOK(r) && r != ';' }

func udfFits(s string) bool {
	wide := false
	n := 0
	for _, r := range s {
		n++
		if r > 255 {
			wide = true
		}
	}
	if wide {
		n *= 2
	}
	return 1+n <= 255
}
func jolietFits(s string) bool { return utf8.RuneCountInString(s) <= 64 }

var windowsDeviceNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

func windowsDeviceName(s string) bool {
	stem := s
	if i := strings.IndexByte(s, '.'); i >= 0 {
		stem = s[:i]
	}
	return windowsDeviceNames[strings.ToUpper(strings.TrimRight(stem, " "))]
}

// validName reports whether s can be recorded verbatim.
func validName(s string, ok func(rune) bool, fits func(string) bool) bool {
	if s == "" || s == "." || s == ".." || !utf8.ValidString(s) || !fits(s) || windowsDeviceName(s) {
		return false
	}
	if last := s[len(s)-1]; last == '.' || last == ' ' {
		return false
	}
	for _, r := range s {
		if !ok(r) {
			return false
		}
	}
	return true
}

// cleanName replaces every unusable character with '_' and fixes names
// Windows cannot open (device names, trailing dots or spaces).
func cleanName(s string, ok func(rune) bool) string {
	s = strings.ToValidUTF8(s, "_")
	r := []rune(s)
	for i, c := range r {
		if !ok(c) {
			r[i] = '_'
		}
	}
	if len(r) > 0 && (r[len(r)-1] == '.' || r[len(r)-1] == ' ') {
		r[len(r)-1] = '_'
	}
	s = string(r)
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	if windowsDeviceName(s) {
		i := strings.IndexByte(s, '.')
		if i < 0 {
			i = len(s)
		}
		s = s[:i] + "_" + s[i:]
	}
	return s
}

// assignNames gives every node in one directory a unique (case-insensitively)
// name accepted by ok and fits. It never fails. It returns the renamed nodes.
func assignNames(nodes []*node, ok func(rune) bool, fits func(string) bool, set func(*node, string)) []*node {
	used := map[string]bool{}
	var pending []*node
	for _, n := range nodes {
		key := strings.ToUpper(n.name)
		if validName(n.name, ok, fits) && !used[key] {
			set(n, n.name)
			used[key] = true
		} else {
			pending = append(pending, n)
		}
	}
	for _, n := range pending {
		clean := cleanName(n.name, ok)
		ext := ""
		if !n.info.IsDir() {
			ext = filepath.Ext(clean)
			if utf8.RuneCountInString(ext) > 16 || ext == clean {
				ext = ""
			}
		}
		stem := []rune(strings.TrimSuffix(clean, ext))
		for attempt := 0; ; attempt++ {
			tail := ext
			if attempt > 0 {
				sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", n.name, attempt)))
				tail = fmt.Sprintf("~%x%s", sum[:6], ext)
			}
			prefix := stem
			for len(prefix) > 0 && !fits(string(prefix)+tail) {
				prefix = prefix[:len(prefix)-1]
			}
			candidate := string(prefix) + tail
			key := strings.ToUpper(candidate)
			if validName(candidate, ok, fits) && !used[key] {
				set(n, candidate)
				used[key] = true
				break
			}
		}
	}
	return pending
}

type renamedEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// assignNames assigns Joliet names (and UDF names in bridge mode) to every
// entry. The full-name namespace is UDF in bridge mode and Joliet otherwise;
// entries renamed there are recorded in p.renamed.
func (p *imagePlan) assignNames() {
	for _, d := range p.dirs {
		aliased := assignNames(d.children, jolietRuneOK, jolietFits, func(n *node, s string) { n.jolietName = s })
		p.legacyAliases += len(aliased)
		primary := aliased
		if p.udf != nil {
			primary = assignNames(d.children, udfRuneOK, udfFits, func(n *node, s string) {
				n.udfName, _ = udfEncode(s)
				n.udfDisplay = s
			})
		}
		for _, n := range primary {
			name := n.jolietName
			if p.udf != nil {
				name = n.udfDisplay
			}
			rel, err := filepath.Rel(p.root.path, n.path)
			if err != nil {
				rel = n.path
			}
			p.renamed = append(p.renamed, renamedEntry{Path: rel, Name: name})
		}
	}
}
