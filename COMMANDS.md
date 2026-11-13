# CLI reference — 1.0.0

All relative paths use the **current working directory**, never the executable's directory. These examples assume `folder2iso` is on PATH. In PowerShell without PATH, use `.\folder2iso.exe`; on Linux/macOS without PATH, use `./folder2iso`.

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

## Format limits

Native ISO 9660 level 3 + standard Joliet + UDF 1.02 by default; no bootable media. Files over 4 GiB are supported. Image size is just under 8 TiB; directory count at most 65,535 including root. UDF names allow 254 Latin-1 or 127 other BMP characters; strict `--format iso` allows 64 BMP characters. No emoji, invalid UTF-8, control characters, U+FEFF/U+FFFE/U+FFFF or reserved `* / : ; ? \`. Unsupported entries cause explicit errors unless deliberately excluded. Host filesystem/free-space/path limits and reader compatibility still apply. There is no unlimited-size switch.
