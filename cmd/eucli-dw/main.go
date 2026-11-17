package main

import (
	"os"

	"eucli-deep-wiki/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
