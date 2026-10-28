package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
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
