package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type imageDetails struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Blocks uint32 `json:"blocks"`
	Label  string `json:"label"`
	UDF    bool   `json:"udf"`
	Joliet bool   `json:"joliet"`
	SHA256 string `json:"sha256,omitempty"`
}

func readImageInfo(path string) (imageDetails, error) {
	d := imageDetails{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return d, err
	}
	defer f.Close()
	inf, err := f.Stat()
	if err != nil {
		return d, err
	}
	if !inf.Mode().IsRegular() || inf.Size() < 19*sector || inf.Size()%sector != 0 {
		return d, errors.New("not a sector-aligned ISO")
	}
	d.Bytes = inf.Size()
	b := make([]byte, sector)
	if _, err := f.ReadAt(b, 16*sector); err != nil {
		return d, err
	}
	if b[0] != 1 || string(b[1:6]) != "CD001" || b[6] != 1 {
		return d, errors.New("missing ISO descriptor")
	}
	d.Blocks = binary.LittleEndian.Uint32(b[80:])
	if d.Blocks != binary.BigEndian.Uint32(b[84:]) || int64(d.Blocks)*sector != d.Bytes {
		return d, errors.New("descriptor size differs from file size")
	}
	if binary.LittleEndian.Uint16(b[128:]) != sector {
		return d, errors.New("unsupported block size")
	}
	d.Label = strings.TrimRight(string(b[40:72]), " \x00")
	if _, err := f.ReadAt(b, 17*sector); err != nil {
		return d, err
	}
	d.Joliet = b[0] == 2 && string(b[1:6]) == "CD001" && string(b[88:91]) == "%/E"
	if d.Bytes >= 22*sector {
		if _, err := f.ReadAt(b, 20*sector); err == nil {
			d.UDF = b[0] == 0 && string(b[1:6]) == "NSR02" && b[6] == 1
		}
	}
	return d, nil
}
func imageCommand(ctx context.Context, args []string, out io.Writer) int {
	mode := args[0]
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(out)
	image := fs.String("image", "", "image path (relative to current directory)")
	checksum := fs.Bool("checksum", false, "calculate SHA-256 (info)")
	expected := fs.String("sha256", "", "expected SHA-256 (verify)")
	asJSON := fs.Bool("json", false, "JSON output, no progress")
	progress := fs.String("progress", "auto", "auto, line/bar, plain (explicit scrolling), none")
	quiet := fs.Bool("quiet", false, "disable progress")
	verbose := fs.Bool("verbose", false, "show detailed image fields")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *image == "" || fs.NArg() != 0 {
		return usageError(out, "--image required; positional arguments unsupported")
	}
	if mode == "verify" && !validHash(*expected) {
		return usageError(out, "verify requires a 64-digit --sha256")
	}
	if mode == "info" && *expected != "" {
		return usageError(out, "--sha256 is verify-only")
	}
	pMode, err := resolveProgress(*progress, out)
	if err != nil {
		return usageError(out, err.Error())
	}
	if *asJSON || *quiet {
		pMode = "none"
	}
	d, err := readImageInfo(*image)
	if err != nil {
		fmt.Fprintln(out, "ERROR:", err)
		return 1
	}
	sink := &outputSink{out: out}
	if mode == "verify" || *checksum {
		p := progressView{sink: sink, prefix: "Hashing " + shortName(filepath.Base(*image)), mode: pMode, started: time.Now(), interval: time.Second}
		d.SHA256, err = hashFile(ctx, *image, p.report)
		if err != nil {
			sink.line("ERROR: %v", err)
			if ctx.Err() != nil {
				return 130
			}
			return 1
		}
	}
	verified := mode == "verify" && strings.EqualFold(d.SHA256, *expected)
	if *asJSON {
		if err := json.NewEncoder(out).Encode(struct {
			imageDetails
			Verified bool `json:"verified"`
		}{d, verified}); err != nil {
			return 1
		}
	} else if mode == "verify" {
		if verified {
			sink.line("Verified %s", d.Path)
		} else {
			sink.line("FAILED %s: SHA-256 mismatch", d.Path)
		}
	} else {
		if *verbose {
			sink.line("Image: %s\nSize: %s (%d bytes)\nVolume: %s\nJoliet: %t\nUDF: %t", d.Path, humanBytes(uint64(d.Bytes)), d.Bytes, d.Label, d.Joliet, d.UDF)
		} else {
			sink.line("%s | %s | %s | Joliet: %t | UDF: %t", d.Path, humanBytes(uint64(d.Bytes)), d.Label, d.Joliet, d.UDF)
		}
		if d.SHA256 != "" {
			sink.line("SHA256 %s", d.SHA256)
		}
	}
	if sink.err != nil {
		return 1
	}
	if mode == "verify" && !verified {
		return 1
	}
	return 0
}
