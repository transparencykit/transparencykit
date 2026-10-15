// Command tkit is the short name for transparencykit (same program).
package main

import (
	"os"

	"github.com/transparencykit/transparencykit/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
