// ee-tunnel — open-source CLI to connect a local Ollama to Entity Enricher.
//
// Source:  https://github.com/TOT-Concept/ee-tunnel
// License: MIT
package main

import (
	"os"

	"github.com/TOT-Concept/ee-tunnel/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
