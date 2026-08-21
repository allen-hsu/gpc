package main

import (
	"os"

	"github.com/allen-hsu/gpc/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
