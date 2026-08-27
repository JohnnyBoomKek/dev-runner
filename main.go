package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	var rootsFlag rootFlags
	flag.Var(&rootsFlag, "root", "project directory or Git repository to scan; repeatable")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	runtime, err := newRuntimeManager("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	roots := runtime.roots(rootsFlag)
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "no discovery roots exist; pass -root /path/to/projects")
		os.Exit(1)
	}
	if args := flag.Args(); len(args) > 0 {
		if err := runCLI(roots, runtime, args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	program := tea.NewProgram(newModel(roots, runtime), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
