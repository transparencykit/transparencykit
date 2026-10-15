// Command vitrine keeps a tamper-evident transparency ledger and publishes
// signed, verifiable transparency reports.
package main

import (
	"os"

	"github.com/exoscale/vitrine/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
