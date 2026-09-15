package main

import (
	"net/http"
	"testing"
)

func TestBuildSourcesIncludesEveryProviderCombination(t *testing.T) {
	sources, err := buildSources(http.DefaultClient, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 12 {
		t.Fatalf("source count = %d", len(sources))
	}
	keys := make(map[string]bool, len(sources))
	for _, source := range sources {
		if keys[source.Key()] {
			t.Fatalf("duplicate source key %q", source.Key())
		}
		keys[source.Key()] = true
	}
	for _, key := range []string{
		"domimaps.lv:latvia:apartments:rent",
		"domimaps.lv:latvia:apartments:sale",
		"domimaps.lv:latvia:houses:rent",
		"domimaps.lv:latvia:houses:sale",
	} {
		if !keys[key] {
			t.Errorf("missing source %q", key)
		}
	}
}

func TestBuildSourcesRejectsInvalidDomimapsDetailURL(t *testing.T) {
	if _, err := buildSources(http.DefaultClient, "ftp://detail.example"); err == nil {
		t.Fatal("invalid DOMImaps detail URL was accepted")
	}
}
