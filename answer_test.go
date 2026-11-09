package main

import (
	"os"
	"os/exec"
	"testing"
)

// macJSC is JavaScriptCore's shell, built into macOS (see docs/tools.md).
const macJSC = "/System/Library/Frameworks/JavaScriptCore.framework/Versions/Current/Helpers/jsc"

// TestAnswerJS runs the table tests of web/answer.js (the answer check runs
// in the browser). Skipped where there is no jsc, e.g. on the Arch laptop.
func TestAnswerJS(t *testing.T) {
	jsc := macJSC
	if _, err := os.Stat(jsc); err != nil {
		if jsc, err = exec.LookPath("jsc"); err != nil {
			t.Skip("jsc not found; the JS answer tests need macOS")
		}
	}
	out, err := exec.Command(jsc, "-m", "testdata/answer_test.js").CombinedOutput()
	if err != nil || len(out) > 0 {
		t.Fatalf("jsc -m testdata/answer_test.js: %v\n%s", err, out)
	}
}
