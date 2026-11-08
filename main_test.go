package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	rev := "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name     string
		version  string
		settings map[string]string
		want     string
	}{
		{"release", "v1.2.0", map[string]string{"vcs.revision": rev, "vcs.modified": "false"}, "v1.2.0 (0123456)"},
		{"dirty", "v1.2.1-0.20261108120000-0123456789ab+dirty", map[string]string{"vcs.revision": rev, "vcs.modified": "true"},
			"v1.2.1-0.20261108120000-0123456789ab+dirty (0123456-dirty)"},
		{"go run", "(devel)", nil, "(devel)"},
		{"no version", "", nil, "(devel)"},
		{"short revision", "v1.0.0", map[string]string{"vcs.revision": "abc"}, "v1.0.0 (abc)"},
	}
	for _, tt := range tests {
		info := &debug.BuildInfo{Main: debug.Module{Version: tt.version}}
		for k, v := range tt.settings {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
		}
		if got := formatVersion(info); got != tt.want {
			t.Errorf("%s: formatVersion = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRunVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := runVersion(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "leitner ") {
		t.Errorf("output = %q", buf.String())
	}
	if err := runVersion([]string{"extra"}, &buf); err == nil {
		t.Error("extra argument accepted")
	}
}

func TestParseServe(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr bool
		want    config
	}{
		{nil, false, config{addr: "127.0.0.1:8370", dbPath: "leitner.db"}},
		{[]string{"-addr", ":9000", "-db", "data/x.db", "-url", "https://h.example.ts.net:8443"}, false,
			config{addr: ":9000", dbPath: "data/x.db", baseURL: "https://h.example.ts.net:8443"}},
		{[]string{"-url", "http://h.example.com"}, true, config{}},
		{[]string{"-url", "https://"}, true, config{}},
		{[]string{"-url", "h.example.com"}, true, config{}},
		{[]string{"extra"}, true, config{}},
		{[]string{"-nope"}, true, config{}},
	}
	for _, tt := range tests {
		got, err := parseServe(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q: err = %v, wantErr %v", tt.args, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("%q: config = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}

func TestRunBackupArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"a.db", "b.db"}} {
		if err := runBackup(args); err == nil || !strings.Contains(err.Error(), "one new file") {
			t.Errorf("%q: err = %v", args, err)
		}
	}
}
