// r9s is a tiny k9s-style TUI for Google Cloud Run. It shells out to gcloud.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pavel-durov/r9s/pkg/cloudrun"
	"github.com/pavel-durov/r9s/pkg/ui"
)

func main() {
	project := flag.String("project", "", "GCP project (skips the project picker)")
	mock := flag.Bool("mock", false, "show built-in fake projects, services and jobs instead of calling gcloud")
	flag.Parse()
	if err := ui.Run(backend(*mock), *project); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// backend picks the data source: built-in demo data with -mock, real gcloud otherwise.
func backend(mock bool) ui.Backend {
	if mock {
		return cloudrun.Demo
	}
	return cloudrun.Gcloud{}
}
