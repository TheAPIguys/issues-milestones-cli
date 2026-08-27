package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheAPIguys/issues-milestones-cli/internal/app"
	"github.com/TheAPIguys/issues-milestones-cli/internal/config"
	"github.com/TheAPIguys/issues-milestones-cli/internal/gh"
)

func main() {
	repo := flag.String("repo", "", "repository to open (OWNER/REPO or HOST/OWNER/REPO)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: i-gh [--repo OWNER/REPO]")
		flag.PrintDefaults()
	}
	flag.Parse()

	storedConfig, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "i-gh: cannot load config: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(
		app.New(gh.NewClient(""), storedConfig, *repo),
		tea.WithAltScreen(),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "i-gh: %v\n", err)
		os.Exit(1)
	}
}
