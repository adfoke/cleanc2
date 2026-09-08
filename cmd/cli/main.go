package main

import (
	"os"

	"cleanc2/internal/cli"
)

func main() {
	r := &cli.Registry{}
	cli.RegisterAll(r)
	os.Exit(r.Run(os.Args[1:]))
}
