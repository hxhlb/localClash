package main

import (
	"os"
	"testing"
)

func TestRunResetDoesNotBootstrapRuntimeFirst(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := run([]string{"reset", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".runtime"); !os.IsNotExist(err) {
		t.Fatalf("reset should run before bootstrap creates .runtime, err=%v", err)
	}
}
