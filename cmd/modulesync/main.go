package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MrEthical07/superapi/internal/devx/modulesync"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: determine current working directory:", err)
		os.Exit(1)
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err != nil {
		fmt.Fprintln(os.Stderr, "error: run this command from repository root")
		os.Exit(1)
	}

	if err := modulesync.Sync(cwd); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println("synced module sql sources into db/schema and db/queries")
}
