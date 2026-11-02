package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// version is stamped by release.sh (-ldflags -X main.version=vX.Y.Z); go install builds report their module version.
var version = "dev"

func versionString() string {
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("claudes", versionString())
		return
	}
	m := newModel()
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if m.chosen == nil {
		return
	}
	// Replace this process with claude so the session resumes in the same pane or tab.
	bin, err := exec.LookPath("claude")
	if err == nil {
		err = os.Chdir(m.chosen.Cwd)
	}
	if err == nil {
		err = syscall.Exec(bin, append([]string{"claude", "--resume", m.chosen.ID}, os.Args[1:]...), os.Environ())
	}
	fmt.Fprintln(os.Stderr, "claudes:", err)
	os.Exit(1)
}
