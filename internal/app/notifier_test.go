package app

import (
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
)

func TestNotificationCaptionUsesPersistedLanguage(t *testing.T) {
	catalog, err := localization.New()
	if err != nil {
		t.Fatal(err)
	}
	notifier := &Notifier{catalog: catalog}
	item := domain.PendingNotification{
		LanguageTag: localization.Russian,
		Listing: domain.Listing{
			Source:       "ss.lv",
			URL:          "https://example.test/listing",
			PropertyType: domain.PropertyApartment,
			DealType:     domain.DealRent,
			Title:        "Квартира",
		},
	}
	caption := notifier.caption(item)
	for _, want := range []string{"Квартира в аренду", "Цена по запросу", "Открыть на SS.lv"} {
		if !strings.Contains(caption, want) {
			t.Errorf("missing %q in %s", want, caption)
		}
	}
}
