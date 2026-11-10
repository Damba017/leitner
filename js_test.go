package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// macJSC is JavaScriptCore's shell, built into macOS (see docs/tools.md).
const macJSC = "/System/Library/Frameworks/JavaScriptCore.framework/Versions/Current/Helpers/jsc"

// TestJS runs the table tests of the app's browser code, testdata/*_test.js
// (answer check, offline practice). Each prints its failures and throws.
// Skipped where there is no jsc, e.g. on the Arch laptop.
func TestJS(t *testing.T) {
	jsc := macJSC
	if _, err := os.Stat(jsc); err != nil {
		if jsc, err = exec.LookPath("jsc"); err != nil {
			t.Skip("jsc not found; the JS tests need macOS")
		}
	}
	files, err := filepath.Glob("testdata/*_test.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("no JS tests found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			out, err := exec.Command(jsc, "-m", f).CombinedOutput()
			if err != nil || len(out) > 0 {
				t.Fatalf("jsc -m %s: %v\n%s", f, err, out)
			}
		})
	}
}
