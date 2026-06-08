package main

import (
	"fmt"
	"os"

	"github.com/umpire-tools/umpire-gen/internal/cli"
)

func main() {
	cfg, err := cli.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	_ = cfg // future stages will use this
}
