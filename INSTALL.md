# Installing claudes (runbook for AI agents)

`claudes` is a single Go binary: a full-screen picker for Claude Code sessions. Install it for the current user.
Never use `sudo`. Stop and tell the user if a step fails; do not improvise around a checksum failure.

## 1. Check prerequisites

- OS is macOS or Linux (`uname -s`), CPU is arm64 or x86_64 (`uname -m`). Nothing else is supported.
- `curl` and `tar` exist. Go and git are only needed if step 2 falls back to building from source.
- The `claude` CLI should be on `PATH` (`command -v claude`); `claudes` execs it to resume sessions. If it is
  missing, still install, then tell the user.

## 2. Install

Run the official installer. It picks the right release tarball, verifies its sha256 against the release's
`checksums.txt`, and installs `claudes` to `~/.local/bin` (it falls back to a source build if no release asset
matches and git + Go are present):

```sh
curl -fsSL https://raw.githubusercontent.com/saimageshvar/claudes/master/install.sh | bash
```

If the user asked for another directory, append `-s -- /that/dir` after `bash`. For a pinned version, prefix
`CLAUDES_VERSION=vX.Y.Z` before `bash`. If the user prefers to read the script first, fetch it with
`curl -fsSL <url> -o install.sh`, show it, then `bash install.sh`.

Alternative when Go is installed and the user prefers it: `go install github.com/saimageshvar/claudes@latest`.

## 3. Verify

```sh
"$HOME/.local/bin/claudes" --version    # prints: claudes vX.Y.Z
```

If `command -v claudes` finds nothing, `~/.local/bin` is not on `PATH`. Add `export PATH="$HOME/.local/bin:$PATH"`
to the user's shell rc (`~/.zshrc` for zsh, `~/.bashrc` for bash) after telling them, and ask them to open a new
shell. Do not run the TUI yourself: it is interactive and needs a real terminal.

## 4. Offer (do not assume)

- The alias `alias cl=claudes`, added to the same rc file.
- Telling the user how to use it: run `claudes`, type to fuzzy-search sessions (it also searches what was said in
  transcripts), press enter to resume in the current pane, `esc` to quit.

## 5. Report

Say which version was installed, where, whether `PATH` needed a change, and whether `claude` was found.

## Uninstall

Remove `claudes` from the install directory and its cache (`~/Library/Caches/claudes` on macOS,
`~/.cache/claudes` on Linux). It writes nothing else.
