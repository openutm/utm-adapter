// Generate the same source used by the adapter's Mission Planner installer.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/openutm/utm-adapter/internal/adapter"
)

func main() {
	output := flag.String("out", "OpenUTMAdapter.cs", "generated plugin source path")
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*output, []byte(adapter.MissionPlannerPluginSource()), 0644); err != nil {
		log.Fatal(err)
	}
}
