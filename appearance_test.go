package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestReleaseNotesShipInVersionedShell(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{assets: assets, buildID: assetsBuildID(assets), rewritten: map[string][]byte{}}
	s.rewriteBuildPlaceholders()
	notes, err := fs.ReadFile(assets, "changelog.html")
	if err != nil {
		t.Fatal(err)
	}
	shell := string(s.rewritten["index.html"])
	if !strings.Contains(shell, string(notes)) || strings.Contains(shell, "__CHANGELOG__") || strings.Contains(shell, "__BUILD__") {
		t.Fatal("shell must contain bundled release notes and resolved build IDs")
	}
	worker := string(s.rewritten["sw.js"])
	for _, asset := range []string{"appearance.css", "changelog.js", "confirm.js", "sheet.js", "settings.js"} {
		url := "/" + asset + "?v=" + s.buildID
		if !strings.Contains(shell, url) || !strings.Contains(worker, url) {
			t.Fatalf("%s must ship in both the HTML and offline precache", asset)
		}
	}
}
