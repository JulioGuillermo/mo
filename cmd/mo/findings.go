package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jg/mo/internal/detect"
)

func printFindings(findings []detect.Finding) {
	if len(findings) == 0 {
		fmt.Println("nothing found")
		return
	}
	table := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "\tPROVIDER\tFOUND IN\tNOTE")
	for _, found := range findings {
		mark := "✓"
		if !found.Usable {
			mark = "✗"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", mark, found.Provider, found.Origin, found.Note)
	}
	table.Flush()
	fmt.Println("\nUse one with mo -p <provider>, or pick it in /settings.")
}
