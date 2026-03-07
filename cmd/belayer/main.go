package main

import (
	"os"

	"github.com/donovan-yohan/belayer/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
