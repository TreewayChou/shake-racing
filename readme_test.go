package main

import (
	"bytes"
	"flag"
	"os"
	"regexp"
	"testing"
)

// Every runtime flag must have a row in the README flags table, so the
// operator never needs `-h` to discover an option.
func TestReadmeDocumentsEveryFlag(t *testing.T) {
	var usage bytes.Buffer
	if _, err := parseFlags([]string{"-h"}, &usage); err != flag.ErrHelp {
		t.Fatalf("parseFlags(-h) err = %v, want flag.ErrHelp", err)
	}
	names := regexp.MustCompile(`(?m)^\s+-(\S+)`).FindAllStringSubmatch(usage.String(), -1)
	if len(names) == 0 {
		t.Fatalf("no flags found in usage output:\n%s", usage.String())
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range names {
		// A row looks like "| `-port <n>` |" or "| `-no-kiosk` |".
		row := regexp.MustCompile("(?m)^\\| `-" + regexp.QuoteMeta(m[1]) + "[` ]")
		if !row.Match(readme) {
			t.Errorf("README.md has no flags-table row for -%s", m[1])
		}
	}
}
