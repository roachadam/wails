# Custom Windows Menu Theme POC

This Windows-focused example demonstrates custom colours flowing from the
existing `MenuBarTheme` into owner-drawn popup and nested-submenu rows.

The Windows build is pure Go; no Windows host, Wails CLI, or CGO toolchain is
required.

```bash
cd v3/examples/custom-menu-theme
go run .
```

Open the **Theme** menu and compare it with the legend in the window:

- Before the custom-popup palette fix, the top menu bar is purple/teal but the
  dropdown uses the stock dark palette.
- After the fix, normal popup rows use `Default`, highlighted rows use
  `Selected`, and disabled text, separators, and check panels are derived from
  those colours.
- Nested submenus use the same palette.
- Windows High Contrast mode continues to override the custom palette.

The example forces Wails dark mode, so it also exercises the original scenario
where the Wails application theme can differ from the Windows system theme.

For a true before/after comparison, copy this POC into a separate module and
build the same source twice. Change only its Wails replacement between a
worktree at `cf69b44b3` and the current working tree:

```bash
go mod edit -replace github.com/wailsapp/wails/v3=/path/to/wails-before/v3
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -ldflags "-H windowsgui -X main.Variant=BEFORE -X main.Commit=cf69b44b3" \
  -o BEFORE-menufix.exe .

go mod edit -replace github.com/wailsapp/wails/v3=/path/to/current/wails/v3
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -ldflags "-H windowsgui -X main.Variant=AFTER -X main.Commit=working-tree" \
  -o AFTER-menufix.exe .
```

The stamped variant and commit are displayed inside each window.
