package telegram

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
)

type wizardState struct {
	OwnerID, UserID, ChatID                   int64
	MessageID                                 int
	Stage                                     string
	LanguageTag                               string
	PropertyTypes                             []domain.PropertyType
	DealType                                  domain.DealType
	AreaKeys, AreaLabels                      []string
	AreaGroup, AreaParentKey, AreaParentLabel string
	AreaChoices                               map[string]string
	PriceMin, PriceMax, RoomsMin, RoomsMax    *int
	AreaMin, AreaMax                          *float64
}
type intOption struct {
	Action   string
	Min, Max *int
}

type floatOption struct {
	Action   string
	Min, Max *float64
}

func ip(v int) *int {
	return &v
}

func fp(v float64) *float64 {
	return &v
}

var rentPrices = []intOption{
	{"any", nil, nil},
	{"max500", nil, ip(500)},
	{"max800", nil, ip(800)},
	{"500to1000", ip(500), ip(1000)}}

var salePrices = []intOption{
	{"any", nil, nil},
	{"max100k", nil, ip(100000)},
	{"max150k", nil, ip(150000)},
	{"100to200k", ip(100000), ip(200000)}}

var rooms = []intOption{
	{"any", nil, nil},
	{"1", ip(1), ip(1)},
	{"2", ip(2), ip(2)},
	{"3", ip(3), ip(3)},
	{"4plus", ip(4), nil}}

var sizes = []floatOption{
	{"any", nil, nil},
	{"30plus", fp(30), nil},
	{"50plus", fp(50), nil},
	{"70plus", fp(70), nil}}

func mainMenuKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonCreateAlert, nil), "menu:new")},
		{button(localizer.Text(localization.ButtonMyAlerts, nil), "menu:filters")},
		{button(localizer.Text(localization.ButtonChangeLanguage, nil), "menu:language")},
	}}
}

func alertsKeyboard(localizer localization.Localizer, filters []domain.SavedFilter) *keyboard {
	rows := [][]inlineButton{}
	for _, f := range filters {
		labelID, action := localization.ButtonStopAlert, "stop"
		if !f.Enabled {
			labelID, action = localization.ButtonRestartAlert, "restart"
		}
		data := map[string]any{"ID": f.ID}
		rows = append(rows, []inlineButton{
			button(localizer.Text(labelID, data), fmt.Sprintf("alert:%s:%d", action, f.ID)),
			button(localizer.Text(localization.ButtonDeleteAlert, data), fmt.Sprintf("alert:delete:%d", f.ID)),
		})
	}
	labelID := localization.ButtonCreateAlert
	if len(filters) > 0 {
		labelID = localization.ButtonCreateAnotherAlert
	}
	rows = append(rows, []inlineButton{button(localizer.Text(labelID, nil), "menu:new")})
	return &keyboard{InlineKeyboard: rows}
}

func deleteAlertKeyboard(localizer localization.Localizer, id string) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonDeletePermanently, nil), "alert:confirm-delete:"+id)},
		{button(localizer.Text(localization.ButtonKeepAlert, nil), "alert:back")},
	}}
}

func languageKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{
		button(localizer.Text(localization.LanguageEnglish, nil), "lang:en"),
		button(localizer.Text(localization.LanguageLatvian, nil), "lang:lv"),
		button(localizer.Text(localization.LanguageRussian, nil), "lang:ru"),
	}}}
}

func wizardStep(localizer localization.Localizer, step int, title, detail string) string {
	return localizer.Text(localization.WizardStep, map[string]any{
		"Step":   step,
		"Title":  html.EscapeString(title),
		"Detail": html.EscapeString(detail),
	})
}

func renderPropertyStep(localizer localization.Localizer) string {
	return wizardStep(localizer, 1, localizer.Text(localization.StepPropertyTitle, nil), localizer.Text(localization.StepPropertyDetail, nil))
}

func renderDealStep(localizer localization.Localizer) string {
	return wizardStep(localizer, 2, localizer.Text(localization.StepDealTitle, nil), localizer.Text(localization.StepDealDetail, nil))
}

func renderAreaStep(localizer localization.Localizer) string {
	return wizardStep(localizer, 3, localizer.Text(localization.StepAreaTitle, nil), localizer.Text(localization.StepAreaDetail, nil))
}

func renderRoomsStep(localizer localization.Localizer) string {
	return wizardStep(localizer, 5, localizer.Text(localization.StepRoomsTitle, nil), localizer.Text(localization.StepRoomsDetail, nil))
}

func renderSizeStep(localizer localization.Localizer) string {
	return wizardStep(localizer, 6, localizer.Text(localization.StepSizeTitle, nil), localizer.Text(localization.StepSizeDetail, nil))
}

func renderPriceStep(localizer localization.Localizer, deal domain.DealType) string {
	detailID := localization.StepPriceRentDetail
	if deal == domain.DealSale {
		detailID = localization.StepPriceSaleDetail
	}
	return wizardStep(localizer, 4, localizer.Text(localization.StepPriceTitle, nil), localizer.Text(detailID, nil))
}

func propertyKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonApartment, nil), "w1:t:apartment")},
		{button(localizer.Text(localization.ButtonHouse, nil), "w1:t:house")},
		{button(localizer.Text(localization.ButtonApartmentOrHouse, nil), "w1:t:both")},
		{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	}}
}

func dealKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonRent, nil), "w1:d:rent"), button(localizer.Text(localization.ButtonBuy, nil), "w1:d:sale")},
		{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	}}
}

func areaScopeKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonAllLatvia, nil), "w1:a:all")},
		{button(localizer.Text(localization.ButtonRiga, nil), "w1:ag:riga")},
		{button(localizer.Text(localization.ButtonJurmala, nil), "w1:ag:jurmala")},
		{button(localizer.Text(localization.ButtonRigaRegion, nil), "w1:ag:riga-region")},
		{button(localizer.Text(localization.ButtonOtherRegions, nil), "w1:ag:other")},
		{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	}}
}

func navigationKeyboard(localizer localization.Localizer, back string) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{
		button(localizer.Text(localization.ButtonBack, nil), back),
		button(localizer.Text(localization.ButtonCancel, nil), "w1:x"),
	}}}
}

func priceKeyboard(localizer localization.Localizer, deal domain.DealType) *keyboard {
	options := rentPrices
	if deal == domain.DealSale {
		options = salePrices
	}
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 2 {
		row := []inlineButton{button(priceOptionLabel(localizer, options[i]), "w1:p:"+options[i].Action)}
		if i+1 < len(options) {
			row = append(row, button(priceOptionLabel(localizer, options[i+1]), "w1:p:"+options[i+1].Action))
		}
		rows = append(rows, row)
	}
	rows = append(rows,
		[]inlineButton{button(localizer.Text(localization.ButtonCustomRange, nil), "w1:p:custom")},
		[]inlineButton{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	)
	return &keyboard{InlineKeyboard: rows}
}

func priceOptionLabel(localizer localization.Localizer, option intOption) string {
	if option.Min == nil && option.Max == nil {
		return localizer.Text(localization.ButtonAnyPrice, nil)
	}

	return formatIntRange(localizer, option.Min, option.Max, "€", "")
}

func roomsKeyboard(localizer localization.Localizer) *keyboard {
	return optionIntKeyboard(localizer, "w1:r:", rooms)
}

func sizeKeyboard(localizer localization.Localizer) *keyboard {
	rows := optionFloatKeyboard(localizer, "w1:s:", sizes).InlineKeyboard
	rows = append(rows, []inlineButton{button(localizer.Text(localization.ButtonCustomRange, nil), "w1:s:custom")})
	return &keyboard{InlineKeyboard: rows}
}

func optionIntKeyboard(localizer localization.Localizer, prefix string, options []intOption) *keyboard {
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 3 {
		row := []inlineButton{}
		for j := i; j < len(options) && j < i+3; j++ {
			label := localizer.Text(localization.ButtonAny, nil)
			if options[j].Min != nil {
				label = groupInt(*options[j].Min)
				if options[j].Max == nil {
					label += "+"
				}
			}
			row = append(row, button(label, prefix+options[j].Action))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")})
	return &keyboard{InlineKeyboard: rows}
}

func optionFloatKeyboard(localizer localization.Localizer, prefix string, options []floatOption) *keyboard {
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 2 {
		row := []inlineButton{button(sizeOptionLabel(localizer, options[i]), prefix+options[i].Action)}
		if i+1 < len(options) {
			row = append(row, button(sizeOptionLabel(localizer, options[i+1]), prefix+options[i+1].Action))
		}
		rows = append(rows, row)
	}
	return &keyboard{InlineKeyboard: rows}
}

func sizeOptionLabel(localizer localization.Localizer, option floatOption) string {
	if option.Min == nil && option.Max == nil {
		return localizer.Text(localization.ButtonAnySize, nil)
	}

	return formatFloatRange(localizer, option.Min, option.Max, "", " m²")
}

func confirmationKeyboard(localizer localization.Localizer) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{
		{button(localizer.Text(localization.ButtonConfirmAlert, nil), "w1:ok")},
		{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	}}
}

func priceOption(deal domain.DealType, action string) (intOption, bool) {
	options := rentPrices
	if deal == domain.DealSale {
		options = salePrices
	}
	for _, o := range options {
		if o.Action == action {
			return o, true
		}
	}
	return intOption{}, false
}

func roomOption(action string) (intOption, bool) {
	for _, o := range rooms {
		if o.Action == action {
			return o, true
		}
	}
	return intOption{}, false
}

func sizeOption(action string) (floatOption, bool) {
	for _, o := range sizes {
		if o.Action == action {
			return o, true
		}
	}
	return floatOption{}, false
}

func (b *Bot) toPrice(ctx context.Context, userID int64, state *wizardState) {
	state.Stage = "price"
	localizer := b.catalog.For(state.LanguageTag)
	b.editState(ctx, userID, state, renderPriceStep(localizer, state.DealType), priceKeyboard(localizer, state.DealType))
}

func (b *Bot) openAreaGroup(ctx context.Context, userID int64, state *wizardState, group string) {
	localizer := b.catalog.For(state.LanguageTag)
	var choices []domain.AreaChoice
	var err error
	switch group {
	case "riga":
		state.AreaParentKey, state.AreaParentLabel = "lv/riga", localizer.Text(localization.ButtonRiga, nil)
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "jurmala":
		state.AreaParentKey, state.AreaParentLabel = "lv/jurmala", localizer.Text(localization.ButtonJurmala, nil)
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "riga-region":
		state.AreaParentKey, state.AreaParentLabel = "lv/rigas-rajons", localizer.Text(localization.ButtonRigaRegion, nil)
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "other":
		state.AreaParentKey = ""
		state.AreaParentLabel = localizer.Text(localization.OtherRegionsTitle, nil)
		choices, err = b.store.ListRootAreas(ctx)
		filtered := choices[:0]
		for _, choice := range choices {
			if choice.Key != "lv/riga" && choice.Key != "lv/jurmala" && choice.Key != "lv/rigas-rajons" {
				filtered = append(filtered, choice)
			}
		}
		choices = filtered
	default:
		return
	}
	if err != nil {
		b.logError(err)
		return
	}
	state.AreaGroup = group
	state.AreaChoices = map[string]string{}
	for _, choice := range choices {
		state.AreaChoices[choice.Key] = choice.Label
	}
	state.AreaKeys = nil
	state.AreaLabels = nil
	state.Stage = "areas"
	b.editState(ctx, userID, state, renderAreaGroup(localizer, state), areaGroupKeyboard(localizer, state))
}

func (b *Bot) toggleArea(ctx context.Context, userID int64, state *wizardState, key string) {
	label, ok := state.AreaChoices[key]
	if !ok {
		return
	}
	index := -1
	for i, current := range state.AreaKeys {
		if current == key {
			index = i
			break
		}
	}
	if index >= 0 {
		state.AreaKeys = append(state.AreaKeys[:index], state.AreaKeys[index+1:]...)
		state.AreaLabels = append(state.AreaLabels[:index], state.AreaLabels[index+1:]...)
	} else {
		state.AreaKeys = append(state.AreaKeys, key)
		state.AreaLabels = append(state.AreaLabels, label)
	}
	localizer := b.catalog.For(state.LanguageTag)
	b.editState(ctx, userID, state, renderAreaGroup(localizer, state), areaGroupKeyboard(localizer, state))
}

func renderAreaGroup(localizer localization.Localizer, state *wizardState) string {
	detail := localizer.Text(localization.AreaGroupDetail, nil)
	if len(state.AreaKeys) > 0 {
		data := map[string]any{"Count": len(state.AreaKeys)}
		detail = localizer.Plural(localization.AreaGroupSelectedDetail, len(state.AreaKeys), data)
	}
	title := localizer.Text(localization.AreasInTitle, map[string]any{"Area": state.AreaParentLabel})
	if state.AreaGroup == "other" {
		title = localizer.Text(localization.OtherRegionsTitle, nil)
	}
	return wizardStep(localizer, 3, title, detail)
}

func areaGroupKeyboard(localizer localization.Localizer, state *wizardState) *keyboard {
	keys := make([]string, 0, len(state.AreaChoices))
	for key := range state.AreaChoices {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return state.AreaChoices[keys[i]] < state.AreaChoices[keys[j]] })
	selected := map[string]bool{}
	for _, key := range state.AreaKeys {
		selected[key] = true
	}
	rows := [][]inlineButton{}
	if state.AreaParentKey != "" {
		label := localizer.Text(localization.AllRegion, map[string]any{"Area": state.AreaParentLabel})
		rows = append(rows, []inlineButton{button(label, "w1:aa")})
	}
	for i := 0; i < len(keys); i += 2 {
		row := []inlineButton{}
		for j := i; j < len(keys) && j < i+2; j++ {
			mark := "· "
			if selected[keys[j]] {
				mark = "✓ "
			}
			row = append(row, button(mark+state.AreaChoices[keys[j]], "w1:at:"+keys[j]))
		}
		rows = append(rows, row)
	}
	selectedData := map[string]any{"Count": len(state.AreaKeys)}
	rows = append(rows,
		[]inlineButton{button(localizer.Plural(localization.ButtonDoneSelected, len(state.AreaKeys), selectedData), "w1:ad")},
		[]inlineButton{button(localizer.Text(localization.ButtonCancel, nil), "w1:x")},
	)
	return &keyboard{InlineKeyboard: rows}
}

func renderConfirmation(localizer localization.Localizer, state *wizardState) string {
	areas := localizer.Text(localization.ButtonAllLatvia, nil)
	if len(state.AreaLabels) > 0 {
		areas = strings.Join(state.AreaLabels, ", ")
	}
	return localizer.Text(localization.ReviewAlert, map[string]any{
		"Property": propertyLabel(localizer, state.PropertyTypes),
		"Deal":     dealLabel(localizer, state.DealType),
		"Area":     html.EscapeString(areas),
		"Price":    formatIntRange(localizer, state.PriceMin, state.PriceMax, "€", ""),
		"Rooms":    formatIntRange(localizer, state.RoomsMin, state.RoomsMax, "", ""),
		"Size":     formatFloatRange(localizer, state.AreaMin, state.AreaMax, "", " m²"),
	})
}

func filtersText(localizer localization.Localizer, filters []domain.SavedFilter) string {
	if len(filters) == 0 {
		return localizer.Text(localization.NoAlerts, nil)
	}
	parts := []string{localizer.Text(localization.MyAlertsHeader, nil)}
	for _, f := range filters {
		state, icon := localizer.Text(localization.StatusActive, nil), "🟢"
		if !f.Enabled {
			state, icon = localizer.Text(localization.StatusPaused, nil), "⏸"
		}
		areas := localizer.Text(localization.ButtonAllLatvia, nil)
		if len(f.AreaLabels) > 0 {
			areas = strings.Join(f.AreaLabels, ", ")
		}
		parts = append(parts, localizer.Text(localization.AlertSummary, map[string]any{
			"Icon":     icon,
			"ID":       f.ID,
			"Status":   state,
			"Property": propertyLabel(localizer, f.PropertyTypes),
			"Deal":     dealLabel(localizer, f.DealType),
			"Area":     html.EscapeString(areas),
			"Price":    formatIntRange(localizer, f.PriceMin, f.PriceMax, "€", ""),
			"Rooms":    formatIntRange(localizer, f.RoomsMin, f.RoomsMax, "", ""),
			"Size":     formatFloatRange(localizer, f.AreaMin, f.AreaMax, "", " m²"),
		}))
	}
	return strings.Join(parts, "\n\n")
}

func propertyLabel(localizer localization.Localizer, types []domain.PropertyType) string {
	if len(types) == 1 && types[0] == domain.PropertyApartment {
		return localizer.Text(localization.PropertyApartments, nil)
	}
	if len(types) == 1 && types[0] == domain.PropertyHouse {
		return localizer.Text(localization.PropertyHouses, nil)
	}
	return localizer.Text(localization.PropertyBoth, nil)
}

func dealLabel(localizer localization.Localizer, deal domain.DealType) string {
	if deal == domain.DealSale {
		return localizer.Text(localization.DealBuy, nil)
	}

	return localizer.Text(localization.DealRent, nil)
}

func formatIntRange(localizer localization.Localizer, minimum, maximum *int, prefix, suffix string) string {
	if minimum == nil && maximum == nil {
		messageID := localization.ButtonAny
		if prefix == "€" {
			messageID = localization.ButtonAnyPrice
		}
		return localizer.Text(messageID, nil)
	}
	if minimum != nil && maximum != nil && *minimum == *maximum {
		return prefix + groupInt(*minimum) + suffix
	}
	if minimum == nil {
		value := prefix + groupInt(*maximum) + suffix
		return localizer.Text(localization.RangeUpTo, map[string]any{"Value": value})
	}
	if maximum == nil {
		return prefix + groupInt(*minimum) + "+" + suffix
	}
	return prefix + groupInt(*minimum) + "–" + prefix + groupInt(*maximum) + suffix
}

func formatFloatRange(localizer localization.Localizer, minimum, maximum *float64, prefix, suffix string) string {
	if minimum == nil && maximum == nil {
		return localizer.Text(localization.ButtonAnySize, nil)
	}
	if minimum != nil && maximum != nil && *minimum == *maximum {
		return prefix + formatFloat(*minimum) + suffix
	}
	if minimum == nil {
		value := prefix + formatFloat(*maximum) + suffix
		return localizer.Text(localization.RangeUpTo, map[string]any{"Value": value})
	}
	if maximum == nil {
		return prefix + formatFloat(*minimum) + "+" + suffix
	}
	return prefix + formatFloat(*minimum) + "–" + prefix + formatFloat(*maximum) + suffix
}

func ParsePriceRange(text string) (*int, *int, error) {
	value := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(text, "–", "-")))
	if strings.HasSuffix(value, "+") {
		value = strings.TrimSuffix(value, "+") + "-"
	}
	parse := func(raw string) (int, error) {
		raw = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "€", ""), " ", "")
		multiplier := 1.0
		if strings.HasSuffix(raw, "k") {
			multiplier = 1000
			raw = strings.TrimSuffix(raw, "k")
			raw = strings.ReplaceAll(raw, ",", ".")
		}
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, err
		}
		result := number * multiplier
		if result != float64(int(result)) {
			return 0, fmt.Errorf("price must resolve to whole euros")
		}
		return int(result), nil
	}
	return parseIntBounds(value, parse)
}

func ParseFloatRange(text string) (*float64, *float64, error) {
	value := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(text, "–", "-")))
	if value == "skip" || value == "all" || value == "any" || value == "-" {
		return nil, nil, nil
	}
	parse := func(raw string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(raw), 64) }
	if !strings.Contains(value, "-") {
		number, err := parse(value)
		return &number, &number, err
	}
	parts := strings.SplitN(value, "-", 2)
	var minimum, maximum *float64
	if strings.TrimSpace(parts[0]) != "" {
		v, err := parse(parts[0])
		if err != nil {
			return nil, nil, err
		}
		minimum = &v
	}
	if strings.TrimSpace(parts[1]) != "" {
		v, err := parse(parts[1])
		if err != nil {
			return nil, nil, err
		}
		maximum = &v
	}
	if minimum != nil && maximum != nil && *minimum > *maximum {
		return nil, nil, fmt.Errorf("minimum cannot be greater than maximum")
	}
	return minimum, maximum, nil
}

func parseIntBounds(value string, parse func(string) (int, error)) (*int, *int, error) {
	if !strings.Contains(value, "-") {
		number, err := parse(value)
		return &number, &number, err
	}
	parts := strings.SplitN(value, "-", 2)
	var minimum, maximum *int
	if strings.TrimSpace(parts[0]) != "" {
		v, err := parse(parts[0])
		if err != nil {
			return nil, nil, err
		}
		minimum = &v
	}
	if strings.TrimSpace(parts[1]) != "" {
		v, err := parse(parts[1])
		if err != nil {
			return nil, nil, err
		}
		maximum = &v
	}
	if minimum != nil && maximum != nil && *minimum > *maximum {
		return nil, nil, fmt.Errorf("minimum cannot be greater than maximum")
	}
	return minimum, maximum, nil
}
