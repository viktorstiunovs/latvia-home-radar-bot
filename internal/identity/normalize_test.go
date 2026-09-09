package identity

import "testing"

func TestNormalizeDescriptionIgnoresPresentationContactsAndBoilerplate(t *testing.T) {
	a := `<p>Mājīgs, gaišs DZĪVOKLIS!</p><p>Vairāk informācijas pa tālruni +371 2000 0000.</p>`
	b := "  majigs gaišs dzīvoklis — "
	if got, want := NormalizeDescription(a), NormalizeDescription(b); got != want {
		t.Fatalf("normalized descriptions differ: %q != %q", got, want)
	}
}

func TestNormalizeAddressFoldsDiacriticsAndStreetAbbreviations(t *testing.T) {
	values := []string{"Brīvības iela 48", " brivibas I. 48 ", "BRĪVĪBAS street, 48"}
	want := NormalizeAddress(values[0])
	for _, value := range values[1:] {
		if got := NormalizeAddress(value); got != want {
			t.Fatalf("NormalizeAddress(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestDescriptionNormalizationRetainsMeaningfulWords(t *testing.T) {
	got := NormalizeDescription("Renovēts dzīvoklis ar balkonu un skatu uz parku")
	if got != "renovets dzivoklis ar balkonu un skatu uz parku" {
		t.Fatalf("unexpected normalized description: %q", got)
	}
}
