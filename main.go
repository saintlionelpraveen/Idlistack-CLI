package main

import (
	"os"

	"github.com/idlistack/cli/cmd"
	"github.com/idlistack/cli/internal/ui"
)

func main() {
	if err := cmd.Execute(); err != nil {
		ui.Error(err.Error())
		os.Exit(1)
	}
}
