# Contributing

## Development

Use Go 1.22 or newer. The executable has no third-party Go modules or external ISO engines. Keep runtime dependencies at zero and retain compatibility with the supported Windows, Linux and macOS targets.

Before submitting a change:

```sh
gofmt -w *.go
go test ./...
go vet ./...
```

For concurrency changes, run `CGO_ENABLED=1 go test -race ./...` on a supported platform with a C toolchain. For changes to image structures, use the independent-reader tests documented in [README.md](README.md#tests-and-validation). A passing encoder unit test alone does not establish interoperability.

Tests that create real files must work on every host filesystem. Linux limits each path component to 255 UTF-8 bytes, while Windows allows 255 UTF-16 code units, so a long CJK name that works on Windows can fail in Linux CI. Keep on-disk test names within 255 bytes; use in-memory names to test longer inputs.

## Changes

Keep pull requests focused. Describe the problem, the resulting behavior and the tests actually run. Update README.md and COMMANDS.md together for CLI changes. Add a regression test for a fixed defect when it provides meaningful coverage.

Preserve current-directory defaults, existence-based batch skipping, minimal terminal output, cancellation cleanup and safe publication. Do not silently truncate source names or relax format constraints without documenting the reader compatibility consequences.

## Bug reports

Include:

- Version, OS and architecture.
- Exact command, exit code and relevant output.
- Whether the output was a terminal, pipe or file.
- A minimal source-tree example, filename lengths and file sizes.
- The reader/device and chosen format for compatibility problems.
- Expected and observed behavior.

Remove private paths and content before sharing. Tiny synthetic files usually reproduce filename/layout problems without exposing original data. Do not upload private ISO images as a default diagnostic step.

## License

Contributions are intended to be distributed under the project's existing [MIT license](LICENSE).
