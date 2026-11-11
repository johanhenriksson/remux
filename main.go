package main

import (
	"os"

	"github.com/johanhenriksson/remux/cmd"
	"github.com/johanhenriksson/remux/shell"
)

func main() {
	// Invoked through the remux-shell symlink: act as a login shell wrapper
	if shell.IsShell(os.Args[0]) {
		shell.Main(os.Args[0], os.Args[1:])
		return
	}
	cmd.Execute()
}
