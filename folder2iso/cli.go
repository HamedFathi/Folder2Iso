package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ", ") }
func (s *stringList) Set(v string) error {
	if v == "" {
		return errors.New("empty pattern")
	}
	if _, err := path.Match(v, ""); err != nil {
		return err
	}
	*s = append(*s, v)
	return nil
}

type options struct {
	format, mode, source, output, outputDir, name, prefix, suffix, label, existing, progress, logPath, reportPath string
	includes, excludeFolders, excludes                                                                            stringList
	dry, overwrite, verify, checksum, quiet, verbose, failFast                                                    bool
	jobs, bufferMiB                                                                                               int
	interval, timeout                                                                                             time.Duration
}
type job struct{ source, target string }
type runReport struct {
	Version    string    `json:"version"`
	Mode       string    `json:"mode"`
	Format     string    `json:"format"`
	Started    time.Time `json:"started"`
	Duration   float64   `json:"duration_seconds"`
	Created    int       `json:"created"`
	Skipped    int       `json:"skipped"`
	Planned    int       `json:"planned"`
	Failed     int       `json:"failed"`
	Cancelled  int       `json:"cancelled"`
	NotStarted int       `json:"not_started"`
	ImageBytes uint64    `json:"created_image_bytes"`
	Results    []result  `json:"results"`
}

var usage = `folder2iso ` + version + `

  folder2iso                             Batch in the current working directory
  folder2iso create --source PATH [--output FILE | --output-dir DIR --name NAME]
  folder2iso batch [--source PARENT] [--output-dir DIR]
  folder2iso info --image FILE [--checksum] [--json]
  folder2iso verify --image FILE --sha256 HEX [--json]
  folder2iso limits
  folder2iso version

Defaults: working directory, one live progress line in terminals, concise summary.
Batch skips existing same-name ISOs; rerun after fixing errors to create only missing images.
Redirected output has no progress stream. Use --verbose for full details.
Legacy --dir remains an alias for batch --source.
`
const formatLimits = `Default: ISO 9660 level 3 + standard Joliet + UDF 1.02 bridge (data, not bootable).
Full names: UDF supports up to 254 Latin-1 characters, or 127 BMP characters
when any character needs 16-bit encoding. No emoji, invalid UTF-8, control
characters, U+FEFF/U+FFFE/U+FFFF, or * / : ; ? \. Unsupported names fail explicitly.
Joliet retains names up to 64 characters; longer names get unique compatibility
aliases. ISO-only readers see generated ASCII aliases. Reader support varies.
--format iso omits UDF and rejects names exceeding 64 BMP characters.
Files over 4 GiB supported. Image limit: just under 8 TiB including metadata.
At most 65,535 directories including root. Host filesystem, free-space, OS path
limits and metadata memory also apply. There is no unlimited mode.`

func resolveProgress(mode string, out io.Writer) (string, error) {
	switch mode {
	case "auto":
		terminal, _ := consoleInfo(out)
		if terminal {
			return "bar", nil
		}
		return "none", nil
	case "line", "bar":
		return "bar", nil
	case "plain", "none":
		return mode, nil
	default:
		return "", errors.New("progress must be auto, line, bar, plain or none")
	}
}
func runAt(parent context.Context, args []string, out io.Writer, currentDir string) int {
	if len(args) > 0 {
		switch args[0] {
		case "help":
			fmt.Fprint(out, usage)
			return 0
		case "limits":
			if len(args) != 1 {
				return usageError(out, "limits takes no arguments")
			}
			fmt.Fprintln(out, formatLimits)
			return 0
		case "version":
			if len(args) != 1 {
				return usageError(out, "version takes no arguments")
			}
			fmt.Fprintln(out, version)
			return 0
		case "info", "verify":
			return imageCommand(parent, args, out)
		}
	}
	o := options{mode: "batch"}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] != "create" && args[0] != "batch" {
			return usageError(out, "unknown command: "+args[0])
		}
		o.mode = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(o.mode, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.format, "format", "bridge", "bridge: ISO/Joliet + UDF long names; iso: strict 64-character Joliet")
	fs.StringVar(&o.source, "source", "", "source folder (create), or parent (batch; default current working directory)")
	legacy := fs.String("dir", "", "legacy alias for batch --source")
	fs.StringVar(&o.output, "output", "", "create: exact output .iso path")
	fs.StringVar(&o.outputDir, "output-dir", "", "output directory; default source parent for create, batch parent for batch")
	fs.StringVar(&o.name, "name", "", "create: image filename; append .iso when absent")
	fs.StringVar(&o.prefix, "prefix", "", "batch: prepend to filenames")
	fs.StringVar(&o.suffix, "suffix", "", "batch: append before .iso")
	fs.StringVar(&o.label, "label", "FOLDER2ISO", "volume label: 1-16 uppercase ASCII letters, digits or underscores")
	fs.StringVar(&o.existing, "existing", "", "skip, error or replace; batch defaults skip, create defaults error")
	fs.BoolVar(&o.overwrite, "overwrite", false, "alias for --existing replace")
	fs.Var(&o.includes, "include-folder", "batch folder name glob; repeatable")
	fs.Var(&o.excludeFolders, "exclude-folder", "batch excluded folder name glob; repeatable")
	fs.Var(&o.excludes, "exclude", "source-relative or basename glob to exclude; repeatable; no **")
	fs.BoolVar(&o.dry, "dry-run", false, "scan/validate/estimate without creating images")
	fs.BoolVar(&o.verify, "verify", false, "reread staged image and compare full SHA-256 before publishing")
	fs.BoolVar(&o.checksum, "checksum", false, "calculate and print full image SHA-256")
	fs.IntVar(&o.jobs, "jobs", 1, "concurrent batch jobs, 1-64; shared progress line")
	fs.IntVar(&o.bufferMiB, "buffer-mib", 4, "buffer per job, 1-256 MiB")
	fs.StringVar(&o.progress, "progress", "auto", "auto: one terminal line, none when redirected; line/bar, plain (explicit scrolling), none")
	fs.DurationVar(&o.interval, "progress-interval", time.Second, "progress interval, minimum 250ms")
	fs.BoolVar(&o.quiet, "quiet", false, "disable progress; keep summary and errors")
	fs.BoolVar(&o.verbose, "verbose", false, "show per-file details, phases and per-image timings")
	fs.BoolVar(&o.failFast, "fail-fast", false, "stop scheduling new jobs after failure; active jobs finish")
	fs.DurationVar(&o.timeout, "timeout", 0, "job-processing timeout; 0 disables")
	fs.StringVar(&o.logPath, "log", "", "append plain details/results to a log, without progress frames")
	fs.StringVar(&o.reportPath, "report", "", "JSON run report; filename must be new")
	showVersion := fs.Bool("version", false, "print version")
	fs.Usage = func() { fmt.Fprint(out, usage); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(out, version)
		return 0
	}
	if fs.NArg() != 0 {
		return usageError(out, "use --source PATH, not positional arguments")
	}
	if *legacy != "" {
		if o.mode != "batch" || o.source != "" {
			return usageError(out, "--dir is batch-only and conflicts with --source")
		}
		o.source = *legacy
	}
	if o.mode == "create" && o.source == "" {
		return usageError(out, "create requires --source (use --source . for the current directory)")
	}
	if o.source == "" {
		o.source = currentDir
	}
	if o.mode == "batch" && (o.output != "" || o.name != "") {
		return usageError(out, "--output/--name are create-only")
	}
	if o.mode == "create" && (o.prefix != "" || o.suffix != "" || len(o.includes) > 0 || len(o.excludeFolders) > 0 || o.jobs != 1) {
		return usageError(out, "batch-only option used with create")
	}
	if o.output != "" && (o.outputDir != "" || o.name != "") {
		return usageError(out, "--output conflicts with --output-dir/--name")
	}
	if o.overwrite {
		if o.existing != "" && o.existing != "replace" {
			return usageError(out, "--overwrite conflicts with --existing")
		}
		o.existing = "replace"
	}
	if o.existing == "" {
		o.existing = "skip"
		if o.mode == "create" {
			o.existing = "error"
		}
	}
	if o.existing != "skip" && o.existing != "error" && o.existing != "replace" {
		return usageError(out, "invalid --existing policy")
	}
	if o.format != "bridge" && o.format != "iso" {
		return usageError(out, "--format must be bridge or iso")
	}
	if o.jobs < 1 || o.jobs > 64 || o.bufferMiB < 1 || o.bufferMiB > 256 {
		return usageError(out, "--jobs must be 1-64; --buffer-mib 1-256")
	}
	if o.interval < 250*time.Millisecond || o.timeout < 0 {
		return usageError(out, "interval must be at least 250ms; timeout cannot be negative")
	}
	if o.quiet && o.verbose {
		return usageError(out, "--quiet conflicts with --verbose")
	}
	var err error
	o.progress, err = resolveProgress(o.progress, out)
	if err != nil {
		return usageError(out, err.Error())
	}
	if o.quiet {
		o.progress = "none"
	}
	if len(o.label) < 1 || len(o.label) > 16 || strings.Trim(o.label, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return usageError(out, "invalid volume label")
	}
	for _, p := range append(append(append(stringList{}, o.includes...), o.excludeFolders...), o.excludes...) {
		if strings.Contains(p, "**") {
			return usageError(out, "** is unsupported; use basename or relative path globs")
		}
	}
	jobs, outputDir, err := collectJobs(o)
	if err != nil {
		fmt.Fprintln(out, "ERROR:", err)
		return 1
	}
	for _, p := range []string{o.logPath, o.reportPath} {
		if p != "" {
			if err := safeAuxiliary(p, jobs); err != nil {
				return usageError(out, err.Error())
			}
		}
	}
	if o.logPath != "" && o.reportPath != "" {
		a, _ := canonical(o.logPath)
		b, _ := canonical(o.reportPath)
		if samePath(a, b) {
			return usageError(out, "log and report must differ")
		}
	}
	if !o.dry {
		if err := os.MkdirAll(outputDir, 0700); err != nil {
			fmt.Fprintln(out, "ERROR:", err)
			return 1
		}
		lock := filepath.Join(outputDir, ".folder2iso.lock")
		if err := os.Mkdir(lock, 0700); err != nil {
			fmt.Fprintln(out, "ERROR: output locked or unavailable:", err, "Remove a stale .folder2iso.lock only when no conversion is active.")
			return 1
		}
		defer os.Remove(lock)
	}
	sink := &outputSink{out: out, verbose: o.verbose}
	var logFile *os.File
	if o.logPath != "" {
		if inf, e := os.Lstat(o.logPath); e == nil && !inf.Mode().IsRegular() {
			return usageError(out, "log must be a regular file")
		}
		logFile, err = os.OpenFile(o.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(out, "ERROR:", err)
			return 1
		}
		defer logFile.Close()
		sink.log = logFile
	}
	var reportFile *os.File
	if o.reportPath != "" {
		reportFile, err = os.OpenFile(o.reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			sink.line("ERROR: report: %v", err)
			return 1
		}
		defer reportFile.Close()
	}
	ctx := parent
	var cancel context.CancelFunc
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, o.timeout)
		defer cancel()
	}
	report := runReport{Version: version, Mode: o.mode, Format: "ISO9660-level3+Joliet", Started: time.Now(), Results: make([]result, len(jobs))}
	if o.format == "bridge" {
		report.Format += "+UDF1.02"
	}
	sink.detail("folder2iso %s | %s | jobs %d | output %s", version, o.mode, o.jobs, outputDir)
	for i, j := range jobs {
		report.Results[i] = result{Source: j.source, Output: j.target, Status: "not_started"}
	}
	var next atomic.Int64
	var stop atomic.Bool
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil || (o.failFast && stop.Load()) {
				return
			}
			index := int(next.Add(1) - 1)
			if index >= len(jobs) {
				return
			}
			j := jobs[index]
			prefix := fmt.Sprintf("[%d/%d]", index+1, len(jobs))
			if inf, e := os.Lstat(j.target); e == nil {
				if o.existing == "skip" {
					report.Results[index].Status = "skipped"
					sink.detail("SKIP %s (exists)", j.target)
					continue
				}
				if o.existing == "error" || !inf.Mode().IsRegular() {
					report.Results[index].Status = "failed"
					report.Results[index].Error = "output exists; replacement needs a regular file and --overwrite"
					sink.line("ERROR %s: %s", j.target, report.Results[index].Error)
					stop.Store(true)
					continue
				}
			} else if !os.IsNotExist(e) {
				report.Results[index].Status = "failed"
				report.Results[index].Error = e.Error()
				sink.line("ERROR %s: %v", j.target, e)
				stop.Store(true)
				continue
			}
			r, e := runJob(ctx, j, o, sink, prefix)
			report.Results[index] = r
			if e != nil {
				sink.line("ERROR %s: %v", j.source, e)
				stop.Store(true)
			} else {
				if o.dry {
					sink.line("PLAN %s -> %s (%s)", j.source, j.target, humanBytes(r.ImageBytes))
				}
				sink.detail("%s %s | %s | duration %s (scan %s, write %s, flush %s, verify %s, publish %s)", strings.ToUpper(r.Status), j.target, humanBytes(r.ImageBytes), duration(r.Duration), duration(r.Scan), duration(r.Write), duration(r.Flush), duration(r.Verify), duration(r.Publish))
				if r.SHA256 != "" {
					if o.checksum {
						sink.line("SHA256 %s  %s", r.SHA256, j.target)
					} else {
						sink.detail("SHA256 %s  %s", r.SHA256, j.target)
					}
				}
			}
		}
	}
	for i := 0; i < o.jobs; i++ {
		wg.Add(1)
		go worker()
	}
	wg.Wait()
	for _, r := range report.Results {
		switch r.Status {
		case "created":
			report.Created++
			report.ImageBytes += r.ImageBytes
		case "planned":
			report.Planned++
		case "skipped":
			report.Skipped++
		case "failed":
			report.Failed++
		case "cancelled":
			report.Cancelled++
		default:
			report.NotStarted++
		}
	}
	aliases := 0
	for _, r := range report.Results {
		if r.Status == "created" || r.Status == "planned" {
			aliases += r.LegacyAliases
		}
	}
	note := ""
	if aliases > 0 {
		note = fmt.Sprintf(" | %d long names: UDF; legacy aliases", aliases)
	}
	report.Duration = time.Since(report.Started).Seconds()
	if o.mode == "create" && report.Created == 1 {
		sink.line("Created %s | %s | %s%s", filepath.Base(report.Results[0].Output), humanBytes(report.ImageBytes), duration(report.Duration), note)
	} else {
		parts := []string{fmt.Sprintf("%d created", report.Created)}
		for _, c := range []struct {
			n     int
			label string
		}{{report.Skipped, "skipped"}, {report.Planned, "planned"}, {report.Failed, "failed"}, {report.Cancelled, "cancelled"}, {report.NotStarted, "not started"}} {
			if c.n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
			}
		}
		sink.line("Done: %s | %s%s", strings.Join(parts, ", "), duration(report.Duration), note)
	}
	code := 0
	if report.Failed > 0 {
		code = 1
	}
	if ctx.Err() != nil {
		code = 130
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = 124
		}
	}
	if reportFile != nil {
		e := json.NewEncoder(reportFile)
		e.SetIndent("", "  ")
		if err := e.Encode(report); err != nil {
			sink.line("ERROR writing report: %v", err)
			code = 1
		} else if err := reportFile.Sync(); err != nil {
			sink.line("ERROR flushing report: %v", err)
			code = 1
		}
	}
	if logFile != nil {
		if err := logFile.Sync(); err != nil {
			fmt.Fprintln(out, "ERROR flushing log:", err)
			code = 1
		}
	}
	if sink.err != nil {
		fmt.Fprintln(out, "ERROR writing output:", sink.err)
		code = 1
	}
	return code
}
func usageError(out io.Writer, s string) int {
	fmt.Fprintln(out, "ERROR:", s, "(use --help)")
	return 2
}
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func inside(root, p string) bool {
	if samePath(root, p) {
		return true
	}
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		p = strings.ToLower(p)
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func canonical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	var tail []string
	current := abs
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent: %s", p)
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}
func matchAny(n string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, n); ok {
			return true
		}
	}
	return false
}
func isoName(n string) (string, error) {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\\x00") {
		return "", fmt.Errorf("invalid image name: %q", n)
	}
	if !strings.EqualFold(filepath.Ext(n), ".iso") {
		n += ".iso"
	}
	return n, nil
}
func collectJobs(o options) ([]job, string, error) {
	source, err := filepath.Abs(o.source)
	if err != nil {
		return nil, "", err
	}
	inf, err := os.Lstat(source)
	if err != nil {
		return nil, "", err
	}
	if !inf.IsDir() || inf.Mode()&os.ModeSymlink != 0 {
		return nil, "", errors.New("source must be a real directory")
	}
	dir := o.outputDir
	if dir == "" {
		dir = source
		if o.mode == "create" {
			dir = filepath.Dir(source)
		}
	}
	var jobs []job
	if o.mode == "create" {
		target := o.output
		if target == "" {
			name := o.name
			if name == "" {
				name = filepath.Base(source) + ".iso"
			}
			name, err = isoName(name)
			if err != nil {
				return nil, "", err
			}
			target = filepath.Join(dir, name)
		} else {
			if !strings.EqualFold(filepath.Ext(target), ".iso") {
				return nil, "", errors.New("--output must end in .iso")
			}
			dir = filepath.Dir(target)
		}
		target, err = filepath.Abs(target)
		if err != nil {
			return nil, "", err
		}
		jobs = []job{{source: source, target: target}}
	} else {
		entries, err := os.ReadDir(source)
		if err != nil {
			return nil, "", err
		}
		out, err := canonical(dir)
		if err != nil {
			return nil, "", err
		}
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() || n == ".folder2iso.lock" || strings.HasPrefix(n, ".folder2iso-") {
				continue
			}
			if len(o.includes) > 0 && !matchAny(n, o.includes) {
				continue
			}
			if matchAny(n, o.excludeFolders) {
				continue
			}
			src := filepath.Join(source, n)
			root, err := canonical(src)
			if err != nil {
				return nil, "", err
			}
			if samePath(root, out) {
				continue
			}
			name, err := isoName(o.prefix + n + o.suffix + ".iso")
			if err != nil {
				return nil, "", err
			}
			target, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				return nil, "", err
			}
			jobs = append(jobs, job{source: src, target: target})
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	out, err := canonical(dir)
	if err != nil {
		return nil, "", err
	}
	roots := make([]string, len(jobs))
	for i, j := range jobs {
		roots[i], err = canonical(j.source)
		if err != nil {
			return nil, "", err
		}
		if inside(roots[i], out) {
			return nil, "", fmt.Errorf("output directory must be outside source: %s", j.source)
		}
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		target, err := canonical(j.target)
		if err != nil {
			return nil, "", err
		}
		key := strings.ToLower(target)
		if seen[key] {
			return nil, "", fmt.Errorf("duplicate/case-colliding output: %s", j.target)
		}
		seen[key] = true
		if inf, err := os.Lstat(j.target); err == nil && (inf.IsDir() || inf.Mode()&os.ModeSymlink != 0) {
			for _, root := range roots {
				if inside(root, target) {
					return nil, "", fmt.Errorf("output resolves inside source: %s", j.target)
				}
			}
		}
	}
	return jobs, dir, nil
}
func safeAuxiliary(p string, jobs []job) error {
	c, err := canonical(p)
	if err != nil {
		return err
	}
	if filepath.Base(c) == ".folder2iso.lock" || strings.HasPrefix(filepath.Base(c), ".folder2iso-") {
		return fmt.Errorf("reserved auxiliary path: %s", p)
	}
	for _, j := range jobs {
		root, err := canonical(j.source)
		if err != nil {
			return err
		}
		target, err := canonical(j.target)
		if err != nil {
			return err
		}
		if inside(root, c) || samePath(target, c) {
			return fmt.Errorf("log/report must be outside sources and distinct from images: %s", p)
		}
	}
	return nil
}
func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
