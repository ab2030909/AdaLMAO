package main

import (
	"os"

	"github.com/adalmao/adalmao/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
