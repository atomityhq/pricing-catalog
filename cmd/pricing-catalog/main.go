package main

import (
	"fmt"
	"os"

	"pricing-catalog/pkg/catalog"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "validate" {
		fmt.Fprintln(os.Stderr, "usage: pricing-catalog validate <snapshot.json>")
		os.Exit(2)
	}

	file, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open snapshot: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	snapshot, err := catalog.LoadJSON(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid snapshot: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("valid snapshot %s: %d records, generated %s\n", snapshot.Version, len(snapshot.Records), snapshot.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"))
}
