package telegram

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

type wizardState struct {
	OwnerID, ChatID                           int64
	MessageID                                 int
	Stage                                     string
	PropertyTypes                             []domain.PropertyType
	DealType                                  domain.DealType
	AreaKeys, AreaLabels                      []string
	AreaGroup, AreaParentKey, AreaParentLabel string
	AreaChoices                               map[string]string
	PriceMin, PriceMax, RoomsMin, RoomsMax    *int
	AreaMin, AreaMax                          *float64
}
type intOption struct {
	Action, Label string
	Min, Max      *int
}
type floatOption struct {
	Action, Label string
	Min, Max      *float64
}

func ip(v int) *int {
	return &v
}

func fp(v float64) *float64 {
	return &v
}

var rentPrices = []intOption{
	{"any", "Any price", nil, nil},
	{"max500", "Up to €500", nil, ip(500)},
	{"max800", "Up to €800", nil, ip(800)},
	{"500to1000", "€500–€1,000", ip(500), ip(1000)}}

var salePrices = []intOption{
	{"any", "Any price", nil, nil},
	{"max100k", "Up to €100k", nil, ip(100000)},
	{"max150k", "Up to €150k", nil, ip(150000)},
	{"100to200k", "€100k–€200k", ip(100000), ip(200000)}}

var rooms = []intOption{
	{"any", "Any", nil, nil},
	{"1", "1", ip(1), ip(1)},
	{"2", "2", ip(2), ip(2)},
	{"3", "3", ip(3), ip(3)},
	{"4plus", "4+", ip(4), nil}}

var sizes = []floatOption{
	{"any", "Any size", nil, nil},
	{"30plus", "30+ m²", fp(30), nil},
	{"50plus", "50+ m²", fp(50), nil},
	{"70plus", "70+ m²", fp(70), nil}}

const customPriceGuide = "Enter a minimum and maximum separated by a hyphen. You can use <code>k</code> for thousands.\n\n<b>Examples</b>\n<code>300-700</code> — From €300 to €700\n<code>-700</code> — From €0 to €700\n<code>500-</code> or <code>500+</code> — From €500 with no maximum\n<code>100000-200000</code> — From €100,000 to €200,000\n<code>100k-200k</code> — Same as €100,000 to €200,000\n<code>100k+</code> — From €100,000 with no maximum"

func mainMenuKeyboard() *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Create alert", "menu:new")}, {button("My alerts", "menu:filters")}}}
}

func alertsKeyboard(filters []domain.SavedFilter) *keyboard {
	rows := [][]inlineButton{}
	for _, f := range filters {
		label, action := "Stop", "stop"
		if !f.Enabled {
			label, action = "Restart", "restart"
		}
		rows = append(rows, []inlineButton{button(fmt.Sprintf("%s #%d", label, f.ID), fmt.Sprintf("alert:%s:%d", action, f.ID)), button(fmt.Sprintf("Delete #%d", f.ID), fmt.Sprintf("alert:delete:%d", f.ID))})
	}
	label := "Create alert"
	if len(filters) > 0 {
		label = "Create another alert"
	}
	rows = append(rows, []inlineButton{button(label, "menu:new")})
	return &keyboard{InlineKeyboard: rows}
}

func deleteAlertKeyboard(id string) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Delete permanently", "alert:confirm-delete:"+id)}, {button("Keep alert", "alert:back")}}}
}

func wizardStep(step int, title, detail string) string {
	return fmt.Sprintf("<b>New alert · Step %d/6</b>\n\n<b>%s</b>\n%s", step, html.EscapeString(title), html.EscapeString(detail))
}

func renderPropertyStep() string {
	return wizardStep(1, "What type of property?", "Choose apartments, houses, or both.")
}

func renderDealStep() string {
	return wizardStep(2, "What are you looking for?", "Choose whether you want to rent or buy.")
}

func renderAreaStep() string {
	return wizardStep(3, "Where should I search?", "Choose all Latvia or open a region to select exact areas.")
}

func renderRoomsStep() string {
	return wizardStep(5, "How many rooms?", "Choose the minimum room requirement.")
}

func renderSizeStep() string {
	return wizardStep(6, "Minimum property size?", "Choose a common minimum or enter a custom range.")
}

func renderPriceStep(deal domain.DealType) string {
	context := "monthly rent"
	if deal == domain.DealSale {
		context = "purchase price"
	}
	return wizardStep(4, "What is your budget?", "Choose a common "+context+" range or enter a custom one.")
}

func propertyKeyboard() *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Apartment", "w1:t:apartment")}, {button("House", "w1:t:house")}, {button("Apartment or house", "w1:t:both")}, {button("Cancel", "w1:x")}}}
}

func dealKeyboard() *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Rent", "w1:d:rent"), button("Buy", "w1:d:sale")}, {button("Cancel", "w1:x")}}}
}

func areaScopeKeyboard() *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("All Latvia", "w1:a:all")}, {button("Rīga", "w1:ag:riga")}, {button("Jūrmala", "w1:ag:jurmala")}, {button("Rīga region", "w1:ag:riga-region")}, {button("Other", "w1:ag:other")}, {button("Cancel", "w1:x")}}}
}

func navigationKeyboard(back string) *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Back", back), button("Cancel", "w1:x")}}}
}

func priceKeyboard(deal domain.DealType) *keyboard {
	options := rentPrices
	if deal == domain.DealSale {
		options = salePrices
	}
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 2 {
		row := []inlineButton{button(options[i].Label, "w1:p:"+options[i].Action)}
		if i+1 < len(options) {
			row = append(row, button(options[i+1].Label, "w1:p:"+options[i+1].Action))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{button("Custom range", "w1:p:custom")}, []inlineButton{button("Cancel", "w1:x")})
	return &keyboard{InlineKeyboard: rows}
}

func roomsKeyboard() *keyboard {
	return optionIntKeyboard("w1:r:", rooms)
}

func sizeKeyboard() *keyboard {
	rows := optionFloatKeyboard("w1:s:", sizes).InlineKeyboard
	rows = append(rows, []inlineButton{button("Custom range", "w1:s:custom")})
	return &keyboard{InlineKeyboard: rows}
}

func optionIntKeyboard(prefix string, options []intOption) *keyboard {
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 3 {
		row := []inlineButton{}
		for j := i; j < len(options) && j < i+3; j++ {
			row = append(row, button(options[j].Label, prefix+options[j].Action))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{button("Cancel", "w1:x")})
	return &keyboard{InlineKeyboard: rows}
}

func optionFloatKeyboard(prefix string, options []floatOption) *keyboard {
	rows := [][]inlineButton{}
	for i := 0; i < len(options); i += 2 {
		row := []inlineButton{button(options[i].Label, prefix+options[i].Action)}
		if i+1 < len(options) {
			row = append(row, button(options[i+1].Label, prefix+options[i+1].Action))
		}
		rows = append(rows, row)
	}
	return &keyboard{InlineKeyboard: rows}
}

func confirmationKeyboard() *keyboard {
	return &keyboard{InlineKeyboard: [][]inlineButton{{button("Confirm alert", "w1:ok")}, {button("Cancel", "w1:x")}}}
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
	b.editState(ctx, userID, state, renderPriceStep(state.DealType), priceKeyboard(state.DealType))
}

func (b *Bot) openAreaGroup(ctx context.Context, userID int64, state *wizardState, group string) {
	var choices []domain.AreaChoice
	var err error
	switch group {
	case "riga":
		state.AreaParentKey, state.AreaParentLabel = "lv/riga", "Rīga"
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "jurmala":
		state.AreaParentKey, state.AreaParentLabel = "lv/jurmala", "Jūrmala"
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "riga-region":
		state.AreaParentKey, state.AreaParentLabel = "lv/rigas-rajons", "Rīga region"
		choices, err = b.store.ListChildAreas(ctx, state.AreaParentKey)
	case "other":
		state.AreaParentKey = ""
		state.AreaParentLabel = "Other regions"
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
	b.editState(ctx, userID, state, renderAreaGroup(state), areaGroupKeyboard(state))
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
	b.editState(ctx, userID, state, renderAreaGroup(state), areaGroupKeyboard(state))
}

func renderAreaGroup(state *wizardState) string {
	detail := "Choose the whole region or select one or more areas."
	if len(state.AreaKeys) > 0 {
		detail = fmt.Sprintf("%d selected. Tap areas to toggle them, then press Done.", len(state.AreaKeys))
	}
	title := "Choose areas in " + state.AreaParentLabel
	if state.AreaGroup == "other" {
		title = "Choose other regions"
	}
	return wizardStep(3, title, detail)
}

func areaGroupKeyboard(state *wizardState) *keyboard {
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
		rows = append(rows, []inlineButton{button("All "+state.AreaParentLabel, "w1:aa")})
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
	rows = append(rows, []inlineButton{button(fmt.Sprintf("Done · %d selected", len(state.AreaKeys)), "w1:ad")}, []inlineButton{button("Cancel", "w1:x")})
	return &keyboard{InlineKeyboard: rows}
}

func renderConfirmation(state *wizardState) string {
	deal := "Rent"
	if state.DealType == domain.DealSale {
		deal = "Buy"
	}
	areas := "All Latvia"
	if len(state.AreaLabels) > 0 {
		areas = strings.Join(state.AreaLabels, ", ")
	}
	return "<b>Review your alert</b>\n\n🏘 <b>Property:</b> " + propertyLabel(state.PropertyTypes) + "\n🔑 <b>Deal:</b> " + deal + "\n📍 <b>Area:</b> " + html.EscapeString(areas) + "\n💶 <b>Price:</b> " + formatIntRange(state.PriceMin, state.PriceMax, "€", "") + "\n🚪 <b>Rooms:</b> " + formatIntRange(state.RoomsMin, state.RoomsMax, "", "") + "\n📐 <b>Size:</b> " + formatFloatRange(state.AreaMin, state.AreaMax, "", " m²") + "\n\nOnly listings discovered after confirmation will be sent."
}

func filtersText(filters []domain.SavedFilter) string {
	if len(filters) == 0 {
		return "<b>No alerts yet</b>\n\nCreate one and I’ll watch new listings for you."
	}
	parts := []string{"<b>My alerts</b>\n<i>Use the buttons below to stop, restart, or delete an alert.</i>"}
	for _, f := range filters {
		state, icon := "Active", "🟢"
		if !f.Enabled {
			state, icon = "Paused", "⏸"
		}
		areas := "All Latvia"
		if len(f.AreaLabels) > 0 {
			areas = strings.Join(f.AreaLabels, ", ")
		}
		parts = append(parts, fmt.Sprintf("%s <b>Alert #%d</b> · <i>%s</i>\n<blockquote>🏘 <b>Property:</b> %s\n🔑 <b>Deal:</b> %s\n📍 <b>Area:</b> %s\n💶 <b>Price:</b> %s\n🚪 <b>Rooms:</b> %s\n📐 <b>Size:</b> %s</blockquote>", icon, f.ID, state, propertyLabel(f.PropertyTypes), map[domain.DealType]string{domain.DealRent: "Rent", domain.DealSale: "Buy"}[f.DealType], html.EscapeString(areas), formatIntRange(f.PriceMin, f.PriceMax, "€", ""), formatIntRange(f.RoomsMin, f.RoomsMax, "", ""), formatFloatRange(f.AreaMin, f.AreaMax, "", " m²")))
	}
	return strings.Join(parts, "\n\n")
}

func propertyLabel(types []domain.PropertyType) string {
	if len(types) == 1 && types[0] == domain.PropertyApartment {
		return "Apartments"
	}
	if len(types) == 1 && types[0] == domain.PropertyHouse {
		return "Houses"
	}
	return "Apartments & houses"
}

func formatIntRange(minimum, maximum *int, prefix, suffix string) string {
	if minimum == nil && maximum == nil {
		return "Any"
	}
	if minimum != nil && maximum != nil && *minimum == *maximum {
		return prefix + groupInt(*minimum) + suffix
	}
	if minimum == nil {
		return "Up to " + prefix + groupInt(*maximum) + suffix
	}
	if maximum == nil {
		return prefix + groupInt(*minimum) + "+" + suffix
	}
	return prefix + groupInt(*minimum) + "–" + prefix + groupInt(*maximum) + suffix
}

func formatFloatRange(minimum, maximum *float64, prefix, suffix string) string {
	if minimum == nil && maximum == nil {
		return "Any"
	}
	if minimum != nil && maximum != nil && *minimum == *maximum {
		return prefix + formatFloat(*minimum) + suffix
	}
	if minimum == nil {
		return "Up to " + prefix + formatFloat(*maximum) + suffix
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
