package localization

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	English = "en"
	Latvian = "lv"
	Russian = "ru"
)

type MessageID string

const (
	Welcome                  MessageID = "Welcome"
	Help                     MessageID = "Help"
	SetupCancelled           MessageID = "SetupCancelled"
	InvalidPriceRange        MessageID = "InvalidPriceRange"
	InvalidSizeRange         MessageID = "InvalidSizeRange"
	LanguagePrompt           MessageID = "LanguagePrompt"
	LanguageChanged          MessageID = "LanguageChanged"
	LanguageEnglish          MessageID = "LanguageEnglish"
	LanguageLatvian          MessageID = "LanguageLatvian"
	LanguageRussian          MessageID = "LanguageRussian"
	ButtonCreateAlert        MessageID = "ButtonCreateAlert"
	ButtonMyAlerts           MessageID = "ButtonMyAlerts"
	ButtonChangeLanguage     MessageID = "ButtonChangeLanguage"
	ButtonStopAlert          MessageID = "ButtonStopAlert"
	ButtonRestartAlert       MessageID = "ButtonRestartAlert"
	ButtonDeleteAlert        MessageID = "ButtonDeleteAlert"
	ButtonCreateAnotherAlert MessageID = "ButtonCreateAnotherAlert"
	ButtonDeletePermanently  MessageID = "ButtonDeletePermanently"
	ButtonKeepAlert          MessageID = "ButtonKeepAlert"
	ButtonCancel             MessageID = "ButtonCancel"
	ButtonBack               MessageID = "ButtonBack"
	ButtonCustomRange        MessageID = "ButtonCustomRange"
	ButtonConfirmAlert       MessageID = "ButtonConfirmAlert"
	ButtonDoneSelected       MessageID = "ButtonDoneSelected"
	InvalidAlert             MessageID = "InvalidAlert"
	AlertNotFound            MessageID = "AlertNotFound"
	AlertRestarted           MessageID = "AlertRestarted"
	AlertStopped             MessageID = "AlertStopped"
	AlertDeleted             MessageID = "AlertDeleted"
	WizardExpired            MessageID = "WizardExpired"
	ChooseAtLeastOneArea     MessageID = "ChooseAtLeastOneArea"
	DeleteAlertConfirm       MessageID = "DeleteAlertConfirm"
	CustomPricePrompt        MessageID = "CustomPricePrompt"
	CustomSizePrompt         MessageID = "CustomSizePrompt"
	AlertCreated             MessageID = "AlertCreated"
	Usage                    MessageID = "Usage"
	AlertPaused              MessageID = "AlertPaused"
	AlertResumed             MessageID = "AlertResumed"
	WizardStep               MessageID = "WizardStep"
	StepPropertyTitle        MessageID = "StepPropertyTitle"
	StepPropertyDetail       MessageID = "StepPropertyDetail"
	StepDealTitle            MessageID = "StepDealTitle"
	StepDealDetail           MessageID = "StepDealDetail"
	StepAreaTitle            MessageID = "StepAreaTitle"
	StepAreaDetail           MessageID = "StepAreaDetail"
	StepRoomsTitle           MessageID = "StepRoomsTitle"
	StepRoomsDetail          MessageID = "StepRoomsDetail"
	StepSizeTitle            MessageID = "StepSizeTitle"
	StepSizeDetail           MessageID = "StepSizeDetail"
	StepPriceTitle           MessageID = "StepPriceTitle"
	StepPriceRentDetail      MessageID = "StepPriceRentDetail"
	StepPriceSaleDetail      MessageID = "StepPriceSaleDetail"
	ButtonApartment          MessageID = "ButtonApartment"
	ButtonHouse              MessageID = "ButtonHouse"
	ButtonApartmentOrHouse   MessageID = "ButtonApartmentOrHouse"
	ButtonRent               MessageID = "ButtonRent"
	ButtonBuy                MessageID = "ButtonBuy"
	ButtonAllLatvia          MessageID = "ButtonAllLatvia"
	ButtonRiga               MessageID = "ButtonRiga"
	ButtonJurmala            MessageID = "ButtonJurmala"
	ButtonRigaRegion         MessageID = "ButtonRigaRegion"
	ButtonOtherRegions       MessageID = "ButtonOtherRegions"
	ButtonAnyPrice           MessageID = "ButtonAnyPrice"
	ButtonAny                MessageID = "ButtonAny"
	ButtonAnySize            MessageID = "ButtonAnySize"
	AreaGroupDetail          MessageID = "AreaGroupDetail"
	AreaGroupSelectedDetail  MessageID = "AreaGroupSelectedDetail"
	AreasInTitle             MessageID = "AreasInTitle"
	OtherRegionsTitle        MessageID = "OtherRegionsTitle"
	AllRegion                MessageID = "AllRegion"
	ReviewAlert              MessageID = "ReviewAlert"
	NoAlerts                 MessageID = "NoAlerts"
	MyAlertsHeader           MessageID = "MyAlertsHeader"
	StatusActive             MessageID = "StatusActive"
	StatusPaused             MessageID = "StatusPaused"
	AlertSummary             MessageID = "AlertSummary"
	PropertyApartments       MessageID = "PropertyApartments"
	PropertyHouses           MessageID = "PropertyHouses"
	PropertyBoth             MessageID = "PropertyBoth"
	DealRent                 MessageID = "DealRent"
	DealBuy                  MessageID = "DealBuy"
	RangeUpTo                MessageID = "RangeUpTo"
	ListingApartment         MessageID = "ListingApartment"
	ListingHouse             MessageID = "ListingHouse"
	ListingForRent           MessageID = "ListingForRent"
	ListingForSale           MessageID = "ListingForSale"
	ListingPriceOnRequest    MessageID = "ListingPriceOnRequest"
	ListingPerMonth          MessageID = "ListingPerMonth"
	ListingRooms             MessageID = "ListingRooms"
	ListingStreet            MessageID = "ListingStreet"
	ListingFloor             MessageID = "ListingFloor"
	ListingSeries            MessageID = "ListingSeries"
	ListingHouseType         MessageID = "ListingHouseType"
	ListingFloors            MessageID = "ListingFloors"
	ListingLandArea          MessageID = "ListingLandArea"
	ListingHeading           MessageID = "ListingHeading"
	ListingViewOn            MessageID = "ListingViewOn"
)

var messageIDs = []MessageID{
	Welcome, Help, SetupCancelled, InvalidPriceRange, InvalidSizeRange, LanguagePrompt, LanguageChanged,
	LanguageEnglish, LanguageLatvian, LanguageRussian, ButtonCreateAlert, ButtonMyAlerts, ButtonChangeLanguage,
	ButtonStopAlert, ButtonRestartAlert, ButtonDeleteAlert, ButtonCreateAnotherAlert, ButtonDeletePermanently,
	ButtonKeepAlert, ButtonCancel, ButtonBack, ButtonCustomRange, ButtonConfirmAlert, ButtonDoneSelected,
	InvalidAlert, AlertNotFound, AlertRestarted, AlertStopped, AlertDeleted, WizardExpired, ChooseAtLeastOneArea,
	DeleteAlertConfirm, CustomPricePrompt, CustomSizePrompt, AlertCreated, Usage, AlertPaused, AlertResumed,
	WizardStep, StepPropertyTitle, StepPropertyDetail, StepDealTitle, StepDealDetail, StepAreaTitle, StepAreaDetail,
	StepRoomsTitle, StepRoomsDetail, StepSizeTitle, StepSizeDetail, StepPriceTitle, StepPriceRentDetail,
	StepPriceSaleDetail, ButtonApartment, ButtonHouse, ButtonApartmentOrHouse, ButtonRent, ButtonBuy,
	ButtonAllLatvia, ButtonRiga, ButtonJurmala, ButtonRigaRegion, ButtonOtherRegions, ButtonAnyPrice, ButtonAny,
	ButtonAnySize, AreaGroupDetail, AreaGroupSelectedDetail, AreasInTitle, OtherRegionsTitle, AllRegion,
	ReviewAlert, NoAlerts, MyAlertsHeader, StatusActive, StatusPaused, AlertSummary, PropertyApartments,
	PropertyHouses, PropertyBoth, DealRent, DealBuy, RangeUpTo, ListingApartment, ListingHouse,
	ListingForRent, ListingForSale, ListingPriceOnRequest, ListingPerMonth, ListingRooms, ListingStreet,
	ListingFloor, ListingSeries, ListingHouseType, ListingFloors, ListingLandArea, ListingHeading, ListingViewOn,
}

var supportedLanguages = []string{English, Latvian, Russian}

//go:embed messages/*.toml
var embeddedMessages embed.FS

type Catalog struct {
	bundle *i18n.Bundle
}

type Localizer struct {
	bundle      *i18n.Bundle
	languageTag string
	localizer   *i18n.Localizer
}

func New() (*Catalog, error) {
	return NewFromFS(embeddedMessages,
		"messages/active.en.toml",
		"messages/active.lv.toml",
		"messages/active.ru.toml",
	)
}

func NewFromFS(fsys fs.FS, files ...string) (*Catalog, error) {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, file := range files {
		if _, err := bundle.LoadMessageFileFS(fsys, file); err != nil {
			return nil, fmt.Errorf("load translation catalog %s: %w", file, err)
		}
	}

	catalog := &Catalog{bundle: bundle}
	if err := catalog.validateEnglish(); err != nil {
		return nil, err
	}

	return catalog, nil
}

func (c *Catalog) For(languageTag string) Localizer {
	normalized := Normalize(languageTag)

	return Localizer{
		bundle:      c.bundle,
		languageTag: normalized,
		localizer:   i18n.NewLocalizer(c.bundle, normalized, English),
	}
}

func (l Localizer) LanguageTag() string {
	return l.languageTag
}

func (l Localizer) Text(id MessageID, data any) string {
	value, err := l.localizer.Localize(&i18n.LocalizeConfig{MessageID: string(id), TemplateData: data})
	if err == nil {
		return value
	}

	fallback, fallbackErr := i18n.NewLocalizer(l.bundle, English).Localize(&i18n.LocalizeConfig{MessageID: string(id), TemplateData: data})
	if fallbackErr == nil {
		return fallback
	}

	return "[" + string(id) + "]"
}

func (l Localizer) Plural(id MessageID, count any, data any) string {
	value, err := l.localizer.Localize(&i18n.LocalizeConfig{MessageID: string(id), PluralCount: count, TemplateData: data})
	if err == nil {
		return value
	}

	fallback, fallbackErr := i18n.NewLocalizer(l.bundle, English).Localize(&i18n.LocalizeConfig{MessageID: string(id), PluralCount: count, TemplateData: data})
	if fallbackErr == nil {
		return fallback
	}

	return "[" + string(id) + "]"
}

func (c *Catalog) Validate() error {
	for _, languageTag := range supportedLanguages {
		localizer := i18n.NewLocalizer(c.bundle, languageTag)
		for _, id := range messageIDs {
			value, resolvedTag, err := localizer.LocalizeWithTag(&i18n.LocalizeConfig{MessageID: string(id), TemplateData: catalogValidationData()})
			if err != nil {
				return fmt.Errorf("validate %s message %s: %w", languageTag, id, err)
			}
			base, _ := resolvedTag.Base()
			if base.String() != languageTag {
				return fmt.Errorf("translation catalog %s is missing message %s", languageTag, id)
			}
			if strings.Contains(value, "<no value>") {
				return fmt.Errorf("translation catalog %s message %s references unknown template data", languageTag, id)
			}
		}
	}

	return nil
}

func Normalize(languageTag string) string {
	tag, err := language.Parse(strings.TrimSpace(languageTag))
	if err != nil {
		return English
	}
	base, _ := tag.Base()

	switch base.String() {
	case English, Latvian, Russian:
		return base.String()
	default:
		return English
	}
}

func SupportedLanguages() []string {
	return append([]string(nil), supportedLanguages...)
}

func MessageIDs() []MessageID {
	return append([]MessageID(nil), messageIDs...)
}

func (c *Catalog) validateEnglish() error {
	localizer := i18n.NewLocalizer(c.bundle, English)
	for _, id := range messageIDs {
		if _, _, err := localizer.LocalizeWithTag(&i18n.LocalizeConfig{MessageID: string(id), TemplateData: catalogValidationData()}); err != nil {
			return fmt.Errorf("English translation %s: %w", id, err)
		}
	}

	return nil
}

func catalogValidationData() map[string]any {
	return map[string]any{
		"Action":   "action",
		"Area":     "area",
		"Command":  "command",
		"Count":    2,
		"Deal":     "deal",
		"Detail":   "detail",
		"Icon":     "icon",
		"ID":       1,
		"Price":    "price",
		"Property": "property",
		"Rooms":    "rooms",
		"Size":     "size",
		"Source":   "source",
		"Status":   "status",
		"Step":     1,
		"Title":    "title",
		"URL":      "https://example.test",
		"Value":    "value",
	}
}
