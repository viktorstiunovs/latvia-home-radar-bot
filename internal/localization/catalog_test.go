package localization

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedCatalogsAreComplete(t *testing.T) {
	catalog, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"en-US": English,
		"lv-LV": Latvian,
		"ru-RU": Russian,
		"de-DE": English,
		"":      English,
		"bogus": English,
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPluralRules(t *testing.T) {
	catalog, err := New()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		languageTag string
		count       int
		want        string
	}{
		{English, 1, "1 room"},
		{English, 2, "2 rooms"},
		{Latvian, 0, "0 istabu"},
		{Latvian, 1, "1 istaba"},
		{Latvian, 2, "2 istabas"},
		{Latvian, 11, "11 istabu"},
		{Latvian, 21, "21 istaba"},
		{Russian, 1, "1 комната"},
		{Russian, 2, "2 комнаты"},
		{Russian, 5, "5 комнат"},
		{Russian, 11, "11 комнат"},
		{Russian, 21, "21 комната"},
	}
	for _, test := range tests {
		data := map[string]any{"Count": test.count}
		got := catalog.For(test.languageTag).Plural(ListingRooms, test.count, data)
		if got != test.want {
			t.Errorf("%s count %d = %q, want %q", test.languageTag, test.count, got, test.want)
		}
	}
}

func TestEnglishFallback(t *testing.T) {
	catalog, err := NewFromFS(embeddedMessages, "messages/active.en.toml")
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.For(Latvian).Text(Welcome, nil)
	if !strings.Contains(got, "Property alerts for Latvia") {
		t.Fatalf("missing English fallback: %q", got)
	}
}

func TestInvalidCatalogFailsToLoad(t *testing.T) {
	files := fstest.MapFS{
		"active.en.toml": {Data: []byte("[Welcome\nother = broken")},
	}
	if _, err := NewFromFS(files, "active.en.toml"); err == nil {
		t.Fatal("expected invalid catalog error")
	}
}
