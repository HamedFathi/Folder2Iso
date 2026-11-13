# Publishing folder2iso

## Repository contents

Extract the distribution and use the **contents of its `folder2iso` directory** as the repository root. README.md, LICENSE and go.mod should be at the top level, not nested one directory below it. Include `.gitignore` when copying files.

The archive is ready to share as a complete distribution. For a Git repository, `.gitignore` excludes `bin/`, locally built executables, ISO images and ZIP archives. Source, tests, documentation and SHA256SUMS.txt remain tracked. SHA256SUMS.txt describes the accompanying release binaries; regenerate it when rebuilding those binaries.

## First push

Create an empty GitHub repository under your account, without a generated README, license or gitignore. From the extracted `folder2iso` directory, run:

```sh
git init
git branch -M main
git add .
git status --short
git commit -m "Initial release: folder2iso 1.0.0"
```

Review `git status` before committing to ensure only the intended project files are included. Replace `YOUR_ACCOUNT` below with the repository owner's account or organization:

```sh
git remote add origin https://github.com/YOUR_ACCOUNT/folder2iso.git
git push -u origin main
```

These commands require Git and authentication to your GitHub account. They are instructions for the maintainer; extracting this ZIP does not create a repository or publish anything automatically. If using an existing repository, integrate the files there instead of reinitializing or replacing its remote/history.

## Releases

Releases are automated by `.github/workflows/release.yml`. Binaries are never committed; `.gitignore` excludes `bin/`, `dist/`, built executables and archives.

To publish a new version:

1. Add an entry for the new version to `CHANGELOG.md` (and optionally bump the default `version` in `folder2iso/main.go`, used for local builds).
2. Commit and push to `main`.
3. Tag the commit and push the tag:

```sh
git tag -a v1.1.0 -m "folder2iso 1.1.0"
git push origin v1.1.0
```

The workflow then runs `go vet` and `go test`, cross-compiles the six targets (Windows, Linux and macOS for amd64 and arm64) with `CGO_ENABLED=0 -trimpath -ldflags='-s -w -X main.version=1.1.0'`, packages each as `folder2iso-<version>-<os>-<arch>.zip` (Windows) or `.tar.gz` (Linux/macOS), generates `SHA256SUMS.txt`, and creates a GitHub Release for the tag with all assets attached. The version printed by `folder2iso version` comes from the tag. Tags with a suffix such as `v1.1.0-rc1` are published as pre-releases.

Use a new version and tag for each release; do not move an already-published version tag. Windows/macOS binaries are cross-compiled and unsigned.
