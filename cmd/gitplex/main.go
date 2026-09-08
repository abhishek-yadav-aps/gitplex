package main

import (
	"fmt"
	"os"

	"github.com/abhishek-yadav-aps/gitplex/internal/gitplex"
)

func main() {
	if err := gitplex.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitplex:", err)
		os.Exit(1)
	}
}
