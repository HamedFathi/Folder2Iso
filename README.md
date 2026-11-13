# Folder2Iso

**Create one ISO per folder with a standalone native command-line tool.**

Folder2Iso is an open-source Go application for converting a single directory or a batch of neighboring directories into separate `.iso` files. Run it in a folder to create `<folder-name>.iso` for each immediate subfolder. Completed images are skipped on the next run.

**Version:** 1.0.0 · **License:** [MIT](LICENSE) · **Runtime dependencies:** none beyond the operating system

## Contents

- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Commands](#commands)
- [Format and long names](#format-and-long-names)
- [Minimal output and progress](#minimal-output-and-progress)
- [Retry after fixing a failed folder](#retry-after-fixing-a-failed-folder)
- [Existing images](#existing-images)
- [Names, labels and batch selection](#names-labels-and-batch-selection)
- [Dry runs, jobs and failure controls](#dry-runs-jobs-and-failure-controls)
- [Checksums, verification, logs and JSON](#checksums-verification-logs-and-json)
- [Exit codes and interruption](#exit-codes-and-interruption)
- [All creation flags](#all-creation-flags)
- [Info and verify flags](#info-and-verify-flags)
- [Format limits](#format-limits)
- [Performance and storage](#performance-and-storage)
- [Troubleshooting](#troubleshooting)
- [Build from source](#build-from-source)
- [Tests and validation](#tests-and-validation)
- [Contributing](#contributing)
- [Publishing and sharing](#publishing-and-sharing)
- [License](#license)

## Features

- Single-folder creation and sequential or parallel batch conversion.
- Current-working-directory defaults, including when installed on `PATH`.
- Native ISO 9660 level 3, Joliet and UDF 1.02 encoding; no external ISO engine, SDK, ADK or Go installation needed to run a binary.
- Long filenames through UDF, with standard Joliet compatibility aliases.
- One shared progress line with percentage, speed and ETA; concise final output.
- Existing-image skipping, explicit replacement, exclusions and dry runs.
- Optional SHA-256 readback verification, text logs and JSON reports.
- Source files stay in place. No automatic Enter prompt after completion.

This creates **data images**, not bootable installers. Compatibility depends on the reader: older ISO/Joliet readers may show aliases for long names. There is no universal or unlimited-size mode.

## Installation

Download the archive for your system from the [Releases page](https://github.com/HamedFathi/Folder2Iso/releases/latest) and extract it:

| Release asset | System | Executable |
| --- | --- | --- |
| `folder2iso-VERSION-windows-amd64.zip` | Windows, Intel/AMD x64 | `folder2iso.exe` |
| `folder2iso-VERSION-windows-arm64.zip` | Windows ARM64 | `folder2iso.exe` |
| `folder2iso-VERSION-linux-amd64.tar.gz` | Linux x64 | `folder2iso` |
| `folder2iso-VERSION-linux-arm64.tar.gz` | Linux ARM64 | `folder2iso` |
| `folder2iso-VERSION-macos-amd64.tar.gz` | Intel macOS | `folder2iso` |
| `folder2iso-VERSION-macos-arm64.tar.gz` | Apple Silicon macOS | `folder2iso` |

Binaries are not stored in the Git repository; they are built and published automatically by GitHub Actions for every version tag. Binaries are unsigned. Linux binaries are statically linked; Windows and macOS use built-in OS interfaces.

### Windows

Copy the selected executable to a permanent directory, for example `C:\Tools\folder2iso`. Add that **directory**, not the executable filename, to your user `Path` environment variable. Open a new terminal, then check:

```cmd
folder2iso version
where folder2iso
```

For the current PowerShell session only:

```powershell
$env:Path += ';C:\Tools\folder2iso'
folder2iso version
```

Without PATH, use the full executable path. In PowerShell, a quoted executable path requires `&`:

```powershell
& 'C:\Tools\folder2iso\folder2iso.exe' batch --source 'D:\Folders'
```

### Linux and macOS

From the extracted distribution directory, install the matching binary. This example is for Linux x64; change the binary directory for your system:

```sh
mkdir -p "$HOME/.local/bin"
cp bin/linux-amd64/folder2iso "$HOME/.local/bin/folder2iso"
chmod +x "$HOME/.local/bin/folder2iso"
export PATH="$HOME/.local/bin:$PATH"
folder2iso version
```

To make PATH persistent, add that `export` line to your shell startup file. Without PATH, execute the selected binary by its absolute path, or use `./folder2iso` from its directory.

### Update or check a download

Replace your installed executable with the matching binary from the new archive. `folder2iso version` prints the release version; `where folder2iso` on Windows or `command -v folder2iso` on Linux/macOS identifies the executable in use.

Each release includes `SHA256SUMS.txt` with hashes of the six release archives. Download it next to your archive, then:

```sh
# Linux
sha256sum -c --ignore-missing SHA256SUMS.txt
# macOS
shasum -a 256 -c --ignore-missing SHA256SUMS.txt
```

On Windows, compare the result with the matching entry in `SHA256SUMS.txt`:

```powershell
Get-FileHash .older2iso-VERSION-windows-amd64.zip -Algorithm SHA256
```

## Quick start

### Convert all immediate subfolders

Windows Command Prompt:

```cmd
cd /d D:\Folders
folder2iso
```

PowerShell:

```powershell
Set-Location 'D:\Folders'
folder2iso
```

Linux/macOS:

```sh
cd /path/to/folders
folder2iso
```

For example:

| Source folder | Created image |
| --- | --- |
| `D:\Folders\Photos` | `D:\Folders\Photos.iso` |
| `D:\Folders\Documents` | `D:\Folders\Documents.iso` |

Nested folders belong inside their parent's image. Loose files in the batch parent are not converted separately. The default uses your terminal's working directory, regardless of the executable's location. Run from an existing terminal rather than relying on a double-click's working directory.

### Convert one folder and choose the image name

```cmd
folder2iso create --source "D:\Photos" --output "E:\Images\Vacation.iso" --verify
```

Linux/macOS equivalent:

```sh
folder2iso create --source "$HOME/Photos" --output "$HOME/Images/Vacation.iso" --verify
```

### Convert the current directory itself

```sh
folder2iso create --source . --output ../CurrentFolder.iso
```

The output must be outside the source tree. All relative paths resolve against the current working directory.

## Commands

Flags follow the command. Paths are supplied with named flags, such as `--source`; positional paths are not supported. Quote paths and patterns containing spaces or shell metacharacters. Boolean flags can be disabled with `--verify=false`; `--verify false` is not valid.

```cmd
folder2iso
folder2iso batch
folder2iso batch --source "D:\Folders" --output-dir "E:\Images"
folder2iso --dir "D:\Folders"
folder2iso create --source "D:\Photos" --output "E:\Images\Vacation.iso"
folder2iso create --source "D:\Photos" --output-dir "E:\Images" --name "Vacation"
folder2iso create --source . --output "E:\Images\CurrentFolder.iso"
folder2iso info --image "E:\Images\Vacation.iso"
folder2iso verify --image "E:\Images\Vacation.iso" --sha256 "REPLACE_WITH_THE_SAVED_64_DIGIT_HASH"
folder2iso limits
folder2iso version
folder2iso help
```

No arguments or bare `batch`: convert each immediate real subfolder of the current directory to `<folder>.iso` in that directory. Nested contents stay inside their parent's image. Batch input is alphabetical; default one worker. `--dir` remains a batch-only alias for `--source`.

`create` still requires `--source`; use `.` for the working directory itself. Default output is beside the source folder. `--output` selects an exact `.iso` path; otherwise use `--output-dir` and/or `--name`. They cannot be combined with `--output`. `.iso` is appended to `--name` when absent. A source folder named `folder.iso` becomes `folder.iso.iso` by default.

Output directories are created when needed. They must be outside selected source trees. If the batch output directory is an immediate child of the input parent, that output child is automatically excluded. Portable case-colliding output names are rejected.

## Format and long names

`--format bridge` is the default for both create and batch, including no arguments. It writes ISO 9660 level 3, standard Joliet and UDF 1.02 directories sharing the same file data.

```cmd
folder2iso
folder2iso create --source "D:\Photos" --format bridge --verify
folder2iso create --source "D:\Legacy" --format iso
folder2iso batch --overwrite --format bridge
```

UDF readers retain full names up to 254 Latin-1 characters, or 127 BMP characters if any character needs 16-bit encoding. Length includes the extension. Joliet readers retain names up to 64 characters; longer names appear as shortened aliases with a deterministic hash suffix. These aliases avoid collisions with existing short names and with each other. The summary reports how many names need this fallback. Basic ISO readers see generated ASCII aliases.

`--format iso` omits UDF and rejects names over 64 characters, matching the old format. Neither choice guarantees compatibility with every reader, device, filename or image size. Already-existing images are still skipped in batch; `--overwrite` explicitly rebuilds them in the selected format.

## Minimal output and progress

Default `--progress auto`:

- Interactive terminal: a single shared row updates in place. Phase changes do not add rows. Long text is clipped before the last terminal column.
- Redirected or piped output: no progress frames; summary and errors only.
- Parallel workers share the same row, showing the latest worker's job number, image and progress.
- Final success clears the live row and prints one concise summary. Individual successful/skipped images are not printed by default.
- `info` prints one metadata line; `verify` prints one verification result. Both follow the same automatic progress policy when hashing.

```cmd
folder2iso --progress line
folder2iso --quiet
folder2iso --verbose
folder2iso --progress plain
folder2iso info --image "Vacation.iso" --checksum --progress line
folder2iso verify --image "Vacation.iso" --sha256 "SAVED_HASH" --quiet
```

`line` and `bar` are equivalent and force carriage-return updates; no ANSI-capable terminal is required. `plain` explicitly requests old-style scrolling progress and is not the default. `none` disables progress. `--verbose` explicitly enables per-file information, phases, scan counts, successful-image details and phase durations. `--quiet` and `--verbose` conflict in creation commands.

`--progress-interval 500ms` adjusts update frequency (minimum 250ms). Percentage/speed/ETA refer to the current write or hash phase, not the entire run. Scan/flush/publish show status without fabricated percentages. In parallel mode the row is not an aggregate byte percentage.

## Retry after fixing a failed folder

Run `folder2iso` again in the same working directory. Default batch mode skips each existing `<folder>.iso` before scanning that source and creates only missing images. A failed folder does not stop later folders unless `--fail-fast` was selected. For a previous run with two successful images and one failed folder, a retry after fixing the folder produces one new image and skips the other two. No special resume flag is needed.

This is existence-based skipping, not partial-file resumption or automatic validation. Explicit `create` retains its existing-target error policy; `create --existing skip` opts into skipping.

## Existing images

| Policy | Behavior |
| --- | --- |
| Batch default | Skip existing paths without validating their contents |
| Create default | Error if target exists |
| `--existing skip` | Skip existing target |
| `--existing error` | Report failure |
| `--existing replace`, `--overwrite` | Replace regular files only after new image creation/flush/optional verification succeeds |

```cmd
folder2iso create --source "D:\Photos" --output "E:\Images\Photos.iso" --overwrite --verify
```

A failed replacement retains the original image. Symlink/directory/special-file destinations are not replaced.

## Names, labels and batch selection

```cmd
folder2iso create --source "D:\Photos" --name "Summer" --output-dir "E:\Images" --label "SUMMER_2026"
folder2iso batch --source "D:\Folders" --prefix "backup-" --suffix "-2026"
folder2iso batch --include-folder "Project*" --exclude-folder "Project-Old"
folder2iso create --source "D:\Project" --exclude ".git" --exclude "cache" --exclude "*.tmp"
```

Volume labels are 1–16 uppercase ASCII letters, digits or underscores, default `FOLDER2ISO`. This changes the disc label, not internal filenames.

Batch folder filters match immediate folder names only. Repeat include patterns for a union; exclusions win. Content `--exclude` is separate: patterns without `/` match a basename at any depth; patterns containing `/` match a source-relative path. Use forward slashes even on Windows, e.g. `--exclude "logs/*.log"`. Matching directories prune their whole subtree. Counts reflect pruned entries, not every descendant. Globs are case-sensitive and support `*`, `?`, and classes like `[ab]`; `**` is rejected. Quote patterns to prevent shell expansion.

## Dry runs, jobs and failure controls

```cmd
folder2iso batch --output-dir "E:\Images" --dry-run
folder2iso batch --source "D:\Folders" --output-dir "E:\Images" --jobs 2
folder2iso batch --fail-fast --timeout 2h
folder2iso create --source "D:\LargeFolder" --buffer-mib 8
```

Dry runs scan/validate and estimate sizes, printing one planned-output line per source. No ISOs/output directories are created. Existing-image policy still applies. Explicitly requested logs/reports are written; their parents must exist in dry-run mode.

Jobs: 1–64, default 1. More workers can help independent fast storage but may slow one disk. Buffer: 1–256 MiB per active worker, default 4 MiB; metadata needs additional memory.

Normally later sources continue after failure. `--fail-fast` stops scheduling new jobs; already active jobs finish. Timeout starts at job processing, default disabled. Cancellation is checked between operations; blocking disk operations can take time to return.

## Checksums, verification, logs and JSON

```cmd
folder2iso create --source "D:\Photos" --checksum
folder2iso create --source "D:\Photos" --verify
folder2iso batch --verify --log "E:\conversion.log" --report "E:\run-001.json"
folder2iso info --image "E:\Images\Photos.iso" --checksum --json
```

`--checksum` prints the complete image SHA-256 calculated while writing. `--verify` also hashes the entire flushed staging image and compares it to the written stream before publication. To keep output minimal, `--verify` alone records its hash in reports/logs but does not print it; combine with `--checksum` to print it.

Standalone `verify --image FILE --sha256 HEX` compares image bytes with a previously saved hash, not with a source tree. `info` identifies the UDF recognition marker as well as Joliet. `info` and standalone `verify` perform basic descriptor/size checks for this writer's unpartitioned ISO layout; they are not universal optical-disc validators. Readback can come from OS caches and is not a snapshot/interoperability guarantee.

The text log is appended to and stores details/results without progress frames. JSON reports require a new path and never overwrite an existing file. Both must be outside source trees and distinct from image paths. Parent directories must exist unless created as the selected output directory. Reports include the selected format and per-image `legacy_name_aliases` counts, and preserve per-job statuses, full paths, counts, bytes, phase timings and hashes independently of the minimal console.

PowerShell verification from a report:

```powershell
$run = Get-Content 'E:\run-001.json' -Raw | ConvertFrom-Json
$image = $run.results | Where-Object status -eq 'created' | Select-Object -First 1
folder2iso verify --image $image.output --sha256 $image.sha256
```

`--json` on info/verify suppresses progress and emits the JSON result. Invalid arguments or I/O failures return a nonzero exit code and an error message.

## Exit codes and interruption

| Code | Meaning |
| --- | --- |
| 0 | Successful creation/planning/verification or skips |
| 1 | Image, verification, filesystem, report or output error |
| 2 | Invalid command/option |
| 124 | Timeout |
| 130 | Cancellation |

Use `%ERRORLEVEL%` in cmd or `$LASTEXITCODE` in PowerShell. Commands never wait for Enter. For interactive use, run them from an existing terminal.

Ctrl+C cleans active temporary output normally. After a forced kill/power loss, remove stale `.folder2iso.lock` / `.folder2iso-*` only after confirming no process is active. A Linux/macOS no-hard-link copy fallback can leave an incomplete final ISO after forced termination; remove that incomplete file before rerunning. Existing paths are skipped, not assumed valid.

## All creation flags

| Flag | Default / scope |
| --- | --- |
| `--format bridge\|iso` | bridge; ISO/Joliet/UDF with full long names |
| `--source PATH` | Required for create; current directory for batch |
| `--dir PATH` | Legacy batch-only alias |
| `--output FILE.iso` | Create-only exact path |
| `--output-dir DIR` | Source parent for create; selected parent for batch |
| `--name NAME` | Create-only; default source name + `.iso` |
| `--prefix TEXT`, `--suffix TEXT` | Batch-only; empty |
| `--label LABEL` | `FOLDER2ISO` |
| `--existing skip\|error\|replace` | skip for batch, error for create |
| `--overwrite` | Alias for replace |
| `--include-folder GLOB`, `--exclude-folder GLOB` | Batch-only; repeatable |
| `--exclude GLOB` | Content exclusions; repeatable |
| `--dry-run` | Off |
| `--verify`, `--checksum` | Off |
| `--jobs N` | 1; multiple workers batch-only |
| `--buffer-mib N` | 4 |
| `--progress auto\|line\|bar\|plain\|none` | auto; one terminal row, none when redirected |
| `--progress-interval DURATION` | 1s, minimum 250ms |
| `--quiet`, `--verbose` | Off |
| `--fail-fast` | Off |
| `--timeout DURATION` | 0, disabled |
| `--log FILE`, `--report FILE` | None |
| `--version` | Print version |
| `--help`, `-h` | Show command help |

Info/verify retain `--image`, `--checksum` (info), `--sha256` (verify), `--json`, and `--progress`. They also accept `--quiet` and `--verbose`. Full help:

```cmd
folder2iso --help
folder2iso create --help
folder2iso batch --help
folder2iso info --help
folder2iso verify --help
folder2iso limits
```

## Info and verify flags

| Flag | Command / purpose |
| --- | --- |
| `--image FILE` | Required for info and verify |
| `--checksum` | Info: calculate and print the image SHA-256 |
| `--sha256 HEX` | Verify: required expected 64-digit SHA-256 |
| `--json` | Emit result JSON and suppress progress |
| `--progress auto\|line\|bar\|plain\|none` | Same progress modes as creation; used while hashing |
| `--quiet` | Disable progress, retain result/errors |
| `--verbose` | Info: show metadata on separate lines |
| `--help`, `-h` | Show command help |

`info --json` includes `verified: false` because inspecting metadata does not compare a supplied checksum. Successful `verify --json` returns `verified: true`. Argument and I/O errors may be plain text even when `--json` is set; check the exit code before parsing the result.

## Format limits

Native ISO 9660 level 3 + standard Joliet + UDF 1.02 by default; no bootable media. Files over 4 GiB are supported. Image size is just under 8 TiB; directory count at most 65,535 including root. UDF names allow 254 Latin-1 or 127 other BMP characters; strict `--format iso` allows 64 BMP characters. No emoji, invalid UTF-8, control characters, U+FEFF/U+FFFE/U+FFFF or reserved `* / : ; ? \`. Unsupported entries cause explicit errors unless deliberately excluded. Host filesystem/free-space/path limits and reader compatibility still apply. There is no unlimited-size switch.

## Performance and storage

File contents stream through a configurable buffer, rather than loading the whole image into RAM. The default buffer is 4 MiB per active job; directory metadata also uses memory proportional to the source tree. ISO, Joliet and UDF share the same payload bytes. Bridge mode adds metadata, not three copies of each file.

Start with the default `--jobs 1`. More jobs may help fast storage but can slow a single hard drive. `--buffer-mib` changes the per-job buffer; larger values do not guarantee higher throughput. Verification requires a full additional read. Actual speed depends on source/destination storage and the number of files.

Images are uncompressed; sparse source files expand to their logical size. Keep enough free space for the full output. Filesystem and OS path limits still apply; FAT32 cannot hold images larger than its file-size limit.

Regular files, hidden files, zero-byte files and nested/empty directories are supported. Internal symlinks and special files fail unless excluded; batch skips top-level symlinked directories. ACLs, extended attributes, alternate data streams, ownership and hard-link identity are not preserved. Keep sources unchanged during conversion: identity, size and modification-time checks do not provide a filesystem snapshot.

Images are staged beside their output and flushed before publication. Windows uses native moves. Linux/macOS use hard links or an exclusive-copy fallback where hard links are unavailable; the fallback requires an extra copy and space for twice the new image. Replacement retains the old regular image until the new image is ready.

## Troubleshooting

| Symptom | Guidance |
| --- | --- |
| Command not found | Add the executable's directory to PATH, reopen the terminal, or use its full path. |
| Wrong executable runs | Check `where folder2iso` or `command -v folder2iso`. |
| Everything is skipped | Batch skips existing output paths. Select `--overwrite` to deliberately recreate images. |
| One folder failed | Fix the reported issue and run again. Completed images remain skipped. |
| Long names appear shortened | The reader is using the ISO/Joliet view. Use a reader that supports the image's UDF filesystem for full names. |
| A filename is rejected | Check `folder2iso limits`. Bridge mode still has encoding and length limits. Rename the entry or explicitly exclude it. |
| Source changed during creation | Stop applications modifying the source and retry. |
| Output is inside the source | Choose an output path beside or outside the source directory. |
| No live progress | Redirected output intentionally has no progress. Use `--progress line` if your terminal wrapper is not detected. |
| Progress scrolls | Remove `--progress plain` and `--verbose`; use `--progress auto` or `line`. |
| Stale lock or temporary directory | Confirm no conversion is active before removing stale `.folder2iso.lock` or `.folder2iso-*` items. |
| Image exists after an interrupted fallback copy | Remove the incomplete image before retrying; existence-based skipping does not validate it. |

## Build from source

Go **1.22 or newer** is needed only to build. The application uses the Go standard library and has no third-party Go modules. Run these commands from the directory containing `go.mod`.

Linux/macOS:

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o folder2iso .
```

Windows PowerShell:

```powershell
go test ./...
go vet ./...
$env:CGO_ENABLED = '0'
go build -buildvcs=false -trimpath -ldflags='-s -w' -o folder2iso.exe .
```

Alternatively, use `sh build.sh` or `build-windows.cmd`. Those scripts run tests and build a binary for the host system. Cross-compilation example from a POSIX shell:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o bin/windows-amd64/folder2iso.exe .
```

Use `GOOS=darwin` for macOS. The six bundled targets use `windows`, `linux` or `darwin`, paired with `amd64` or `arm64`.

## Tests and validation

```sh
go test ./...
go vet ./...
# Requires a supported race-detector platform and a working C toolchain:
CGO_ENABLED=1 go test -race ./...
```

Tests cover CLI defaults, relative paths, progress rendering, retries, exclusions, overwrite safety, logs/reports, cancellation, descriptor checksums and alias collisions. Optional independent readers are test tools only; they are not runtime dependencies:

```sh
# Requires xorriso on PATH for ISO/Joliet extraction tests.
go test -v ./...

# Requires Python 3 and system libudfread for UDF tests.
FOLDER2ISO_UDF_TEST=1 go test -v ./...

# Adds a complete >4 GiB round trip; requires xorriso and substantial disk space.
FOLDER2ISO_UDF_TEST=1 FOLDER2ISO_LARGE_TEST=1 go test -v ./...
```

Independent extraction was checked on Linux with xorriso and libudfread, including long Unicode names and a 4 GiB + 123-byte file. The large test needs over 12 GiB of logical temporary storage. A separate sparse 700 GiB test checks allocation-chain boundaries; it does not write or benchmark 700 GiB of payload.

Linux tests, race detection and static checks passed for the supplied implementation. Windows/macOS binaries are cross-compiled; native Windows/macOS execution and real exFAT-mounted operation have not been tested in this environment. Compatibility with every optical-disc reader is not claimed.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development checks and useful bug-report details. Small, focused changes with reproducible examples are easiest to review. Preserve native operation and document CLI or format changes.

## Publishing and sharing

Pushing a version tag such as `v1.1.0` triggers a GitHub Actions workflow that builds all six binaries and publishes them, with `SHA256SUMS.txt`, as a GitHub Release. [PUBLISHING.md](PUBLISHING.md) describes the release process. Generated binaries are excluded by `.gitignore` and are not part of source history.

[COMMANDS.md](COMMANDS.md) provides a standalone CLI reference. [CHANGELOG.md](CHANGELOG.md) records the 1.0.0 release contents.

## License

[MIT](LICENSE). The license notice is included in the source and in every release archive.
