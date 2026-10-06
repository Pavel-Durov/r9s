// r9s is a tiny k9s-style TUI for Google Cloud Run. It shells out to gcloud.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pavel-durov/r9s/pkg/ui"
)

func main() {
	project := flag.String("project", "", "GCP project (skips the project picker)")
	flag.Parse()
	if err := ui.Run(*project); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
