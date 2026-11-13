package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
)

// All workers share one live row. Routine job details are only printed with verbose.
type outputSink struct {
	mu            sync.Mutex
	out, log      io.Writer
	err           error
	active        bool
	lastWidth     int
	verbose       bool
	widthOverride int
}

func (s *outputSink) note(err error) {
	if s.err == nil && err != nil {
		s.err = err
	}
}
func (s *outputSink) width() int {
	if s.widthOverride > 0 {
		return s.widthOverride
	}
	_, n := consoleInfo(s.out)
	return n
}
func cellWidth(r rune) int {
	if unicode.IsMark(r) {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe10 && r <= 0xfe6f) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || r >= 0x1f300) {
		return 2
	}
	return 1
}
func fitLine(text string, width int) (string, int) {
	if width < 1 {
		width = 1
	}
	var b strings.Builder
	n := 0
	for _, r := range text {
		if unicode.IsControl(r) {
			r = ' '
		}
		w := cellWidth(r)
		if n+w > width {
			break
		}
		b.WriteRune(r)
		n += w
	}
	return b.String(), n
}
func (s *outputSink) clear() {
	if !s.active {
		return
	}
	n := s.lastWidth
	if max := s.width() - 1; n > max {
		n = max
	}
	if n < 0 {
		n = 0
	}
	_, err := fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", n))
	s.note(err)
	s.active = false
	s.lastWidth = 0
}
func (s *outputSink) line(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
	text := fmt.Sprintf(format, args...)
	_, err := fmt.Fprintln(s.out, text)
	s.note(err)
	if s.log != nil {
		_, err = fmt.Fprintln(s.log, text)
		s.note(err)
	}
}
func (s *outputSink) detail(format string, args ...any) {
	if s.verbose {
		s.line(format, args...)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.log != nil {
		_, err := fmt.Fprintf(s.log, format+"\n", args...)
		s.note(err)
	}
}
func (s *outputSink) progress(text, mode string) {
	if mode == "none" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "plain" {
		s.clear()
		_, err := fmt.Fprintln(s.out, text)
		s.note(err)
		return
	}
	limit := s.width() - 1
	line, width := fitLine(text, limit)
	old := s.lastWidth
	if old > limit {
		old = limit
	}
	pad := old - width
	if pad < 0 {
		pad = 0
	}
	_, err := fmt.Fprintf(s.out, "\r%s%s", line, strings.Repeat(" ", pad))
	s.note(err)
	s.active = true
	s.lastWidth = width
}

type progressView struct {
	sink          *outputSink
	prefix, mode  string
	started, last time.Time
	interval      time.Duration
	complete      bool
}

func (p *progressView) report(done, total uint64) {
	if p.mode == "none" || p.complete {
		return
	}
	now := time.Now()
	if done != total && !p.last.IsZero() && now.Sub(p.last) < p.interval {
		return
	}
	p.last = now
	secs := now.Sub(p.started).Seconds()
	if secs < 0.001 {
		secs = 0.001
	}
	rate := float64(done) / secs
	percent := 100.0
	if total > 0 {
		percent = 100 * float64(done) / float64(total)
	}
	eta := "--"
	if rate > 0 && total >= done {
		eta = duration(float64(total-done) / rate)
	}
	p.sink.progress(fmt.Sprintf("%s %5.1f%% | %.1f MiB/s | ETA %s", p.prefix, percent, rate/(1<<20), eta), p.mode)
	if done == total {
		p.complete = true
	}
}
func humanBytes(n uint64) string {
	v := float64(n)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < 4 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}
func duration(s float64) string {
	d := time.Duration(s * float64(time.Second))
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
func shortName(name string) string {
	r := []rune(name)
	if len(r) > 24 {
		return string(r[:21]) + "..."
	}
	return name
}
