package main

import (
	"fmt"
	"os"

	"github.com/juspay/gitplex/internal/gitplex"
)

func main() {
	if err := gitplex.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitplex:", err)
		os.Exit(1)
	}
}
