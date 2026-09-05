package telegram

import (
	"strings"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
)

func TestLocalizedWizardFlow(t *testing.T) {
	tests := []struct {
		languageTag string
		stepText    string
		buttonText  string
		areaText    string
	}{
		{localization.English, "What type of property?", "Apartment", "Area:</b> A&amp;B"},
		{localization.Latvian, "Kādu īpašuma veidu meklējat?", "Dzīvoklis", "Vieta:</b> A&amp;B"},
		{localization.Russian, "Какой тип недвижимости?", "Квартира", "Место:</b> A&amp;B"},
	}
	for _, test := range tests {
		localizer := testLocalizer(t, test.languageTag)
		if got := renderPropertyStep(localizer); !strings.Contains(got, test.stepText) {
			t.Errorf("%s property step = %q", test.languageTag, got)
		}
		if got := propertyKeyboard(localizer).InlineKeyboard[0][0].Text; got != test.buttonText {
			t.Errorf("%s property button = %q, want %q", test.languageTag, got, test.buttonText)
		}

		state := &wizardState{
			LanguageTag:   test.languageTag,
			PropertyTypes: []domain.PropertyType{domain.PropertyApartment},
			DealType:      domain.DealRent,
			AreaLabels:    []string{"A&B"},
		}
		if got := renderConfirmation(localizer, state); !strings.Contains(got, test.areaText) {
			t.Errorf("%s confirmation did not localize and escape area: %q", test.languageTag, got)
		}
	}
}

func TestLocalizedAlertSummaryAndKeyboard(t *testing.T) {
	localizer := testLocalizer(t, localization.Russian)
	filters := []domain.SavedFilter{{
		ID:            7,
		DealType:      domain.DealSale,
		PropertyTypes: []domain.PropertyType{domain.PropertyHouse},
		AreaLabels:    []string{"Rīga & apkārtne"},
		Enabled:       false,
	}}
	text := filtersText(localizer, filters)
	for _, want := range []string{"Мои уведомления", "Приостановлено", "Дома", "Покупка", "Rīga &amp; apkārtne"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	keyboard := alertsKeyboard(localizer, filters)
	if got := keyboard.InlineKeyboard[0][0].Text; got != "Возобновить #7" {
		t.Fatalf("unexpected restart button: %q", got)
	}
	if got := keyboard.InlineKeyboard[0][1].CallbackData; got != "alert:delete:7" {
		t.Fatalf("callback protocol changed: %q", got)
	}
}

func TestLanguageKeyboardAndSupportedValues(t *testing.T) {
	localizer := testLocalizer(t, localization.Latvian)
	keyboard := languageKeyboard(localizer)
	got := keyboard.InlineKeyboard[0]
	if len(got) != 3 || got[0].Text != "English" || got[1].Text != "Latviešu" || got[2].Text != "Русский" {
		t.Fatalf("unexpected language keyboard: %+v", got)
	}
	for _, languageTag := range localization.SupportedLanguages() {
		if !supportedLanguage(languageTag) {
			t.Errorf("supported language rejected: %s", languageTag)
		}
	}
	if supportedLanguage("de") {
		t.Fatal("unsupported language accepted")
	}
}

func TestRussianAnyRoomsLabel(t *testing.T) {
	localizer := testLocalizer(t, localization.Russian)
	state := &wizardState{
		LanguageTag:   localization.Russian,
		PropertyTypes: []domain.PropertyType{domain.PropertyApartment},
		DealType:      domain.DealRent,
	}
	if got := renderConfirmation(localizer, state); !strings.Contains(got, "Комнаты:</b> Любое количество") {
		t.Fatalf("unexpected any-rooms summary: %s", got)
	}
	if got := roomsKeyboard(localizer).InlineKeyboard[0][0].Text; got != "Любое количество" {
		t.Fatalf("unexpected any-rooms button: %q", got)
	}
}
