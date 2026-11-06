// Command reservelint is the CLI entry point.
package main

import (
	"os"

	"github.com/jegorscode/reservelint/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
