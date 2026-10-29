# claudes

A full-screen picker for Claude Code sessions. Find a session and press enter: `claudes` changes to the
session's folder and replaces itself with `claude --resume <id>`, in the same pane or tab.

Sessions come from `~/.claude/projects/*/*.jsonl`. Titles, folders, branches, worktree info and counts are
cached in `~/Library/Caches/claudes/index-v6.json` (the OS cache dir), so launch is instant after the first run.

## Keys

| Key | Action |
|---|---|
| type | filter immediately; space-separated words must all match title, folder, branch or worktree name; 2+ letters also search everything said in the transcript |
| `↑` `↓` | move |
| `pgup` `pgdown` | previous / next page |
| `enter` | resume the selected session |
| `tab` / `shift+tab` | cycle the filter tabs: All, Today, This week, Worktrees |
| `ctrl+p` | show / hide the preview (wide terminals, 110+ columns) |
| `esc` | clear the search; quit when it is already empty |
| `ctrl+c` | quit |
| `shift+↑` `shift+↓` | scroll the preview's "latest" chat |
| mouse | wheel over the preview's top pane ("how it started") scrolls down to newer messages, over the bottom pane ("latest") scrolls up to older ones; click a row to select it, double-click to resume, click a tab to switch, wheel to scroll |

## Development

```sh
go test ./...            # layout goldens at 80, 110 and 140 columns, plus a smoke test on your real sessions
go test -run TestLayout -update ./...   # rewrite testdata/*.golden after an intended layout change
```
