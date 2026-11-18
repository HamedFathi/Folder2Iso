# Changelog

## 1.0.1

- Filenames no longer fail a conversion. UDF keeps `;` and every other name Windows can hold verbatim; names a namespace cannot store are sanitised, shortened and de-duplicated deterministically, printed as `WARN renamed` and listed under `renamed_entries` in JSON reports.
- `--format iso` aliases long names instead of rejecting them.
- Joliet aliases no longer inherit characters Joliet forbids.

## 1.0.0

- Native Go encoder for ISO 9660 level 3, Joliet and UDF 1.02 bridge images.
- Single-folder and batch commands with custom output paths, names and labels.
- Current-working-directory defaults and existing-output skipping in batch mode.
- Long filenames preserved in UDF with collision-safe Joliet compatibility aliases.
- Strict ISO/Joliet mode through `--format iso`.
- Single-line terminal progress and concise output when redirected.
- Exclusions, dry runs, concurrent jobs, timeouts and fail-fast scheduling.
- Optional SHA-256 checksums/readback verification, logs and JSON reports.
- Standalone `info`, `verify`, `limits`, `version` and help commands.
- Windows, Linux and macOS binaries for amd64 and arm64.
- Source, tests, complete usage documentation and MIT license.

The release retains version 1.0.0 as requested. Earlier development archives also used this version; the final package includes `--format bridge|iso` and the GitHub publishing documentation. Use the included binary checksums to identify this distribution precisely.
