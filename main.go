// Command zen-board renders a narrated whiteboard video from a plain-text .zen
// script. See AGENTS.md for the agent-facing loop, and internal/cli for the verbs.
package main

import (
	"os"

	"zen-board/internal/cli"
)

func main() {
	os.Exit(cli.New().Run(os.Args[1:]))
}

// Run executes one pipeline invocation in-process. It is the historical entry
// point of this file and is kept as the API the integration suite drives: the
// process must never exit from here, because the tests call it repeatedly in one
// process. Exit codes belong to main.
func Run() error {
	return cli.New().RunInProcess(os.Args[1:])
}
