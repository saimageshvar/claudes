# claudes

A full-screen picker for Claude Code sessions. Find a session and press enter: `claudes` changes to the
session's folder and replaces itself with `claude --resume <id>`, in the same pane or tab.

Sessions come from `~/.claude/projects/*/*.jsonl`. Titles, folders, branches, worktree info and counts are
cached in `~/Library/Caches/claudes/index-v6.json` (the OS cache dir), so launch is instant after the first run.

## Install

macOS or Linux, Intel or ARM. One command (downloads the release binary, verifies its checksum, installs to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/saimageshvar/claudes/master/install.sh | bash
```

Then run `claudes`. Tips:

- Another directory: `... | bash -s -- /usr/local/bin`. A specific version: `... | CLAUDES_VERSION=v0.1.0 bash`.
- If `~/.local/bin` is not on your `PATH`, the installer prints the line to add.
- Handy alias: `alias cl=claudes`.
- With Go instead: `go install github.com/saimageshvar/claudes@latest`.
- Update by running the install command again. Uninstall with `rm -r ~/.local/bin/claudes ~/Library/Caches/claudes` (the cache is `~/.cache/claudes` on Linux).

### Let an AI agent install it

Paste this into Claude Code (or any coding agent with shell access):

```text
Install claudes on this machine: read https://raw.githubusercontent.com/saimageshvar/claudes/master/INSTALL.md and follow it. Tell me what you installed and where.
```

`INSTALL.md` is the agent's runbook: it checks prerequisites, runs the installer (or builds from source if there is no release for the platform), verifies `claudes --version`, fixes `PATH`, and offers the `cl` alias.

### Releasing (maintainers)

Push a tag, or run `./release.sh vX.Y.Z` locally with `gh`: it cross-builds the four platform tarballs, writes `checksums.txt`, and publishes the GitHub release. `.github/workflows/release.yml` does the same on any pushed `v*` tag.

## Keys

| Key | Action |
|---|---|
| type | filter immediately; space-separated words must all match title, folder, branch or worktree name; 2+ letters also search everything said in the transcript; the preview then shows a `matches` band with up to 5 matching lines, tagged by who said them |
| `↑` `↓` | move |
| `pgup` `pgdown` | previous / next page |
| `enter` | resume the selected session (the key hints and page position live in the two top rows) |
| `tab` / `shift+tab` | cycle the filter tabs: All, Today, This week, Worktrees |
| `ctrl+p` | show / hide the preview (wide terminals, 110+ columns) |
| `esc` | clear the search; quit when it is already empty |
| `ctrl+c` | quit |
| `shift+↑` `shift+↓` | scroll the preview's "latest" chat |
| mouse | wheel over the preview's `matches` band scrolls its lines, over the preview's top pane ("how it started") scrolls down to newer messages, over the bottom pane ("latest") scrolls up to older ones; click a row to select it, double-click to resume, click a tab to switch, wheel to scroll |

## Development

```sh
go test ./...            # layout goldens at 80, 110 and 140 columns, plus a smoke test on your real sessions
go test -run TestLayout -update ./...   # rewrite testdata/*.golden after an intended layout change
```
