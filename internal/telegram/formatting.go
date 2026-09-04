package telegram

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

func FormatListing(l domain.Listing) string {
	property, icon := "Apartment", "🏢"
	if l.PropertyType == domain.PropertyHouse {
		property, icon = "House", "🏡"
	}
	action := "for rent"
	if l.DealType == domain.DealSale {
		action = "for sale"
	}
	price := "Price on request"
	if l.PriceEUR != nil {
		price = "€" + groupInt(*l.PriceEUR)
		if l.DealType == domain.DealRent {
			price += " / month"
		}
	}
	details := []string{}
	location := ""
	if l.PropertyType == domain.PropertyHouse && l.City != "" {
		location = joinUnique(l.City, l.District)
	} else if l.City != "" {
		location = joinUnique(l.District, l.City)
	} else if l.AreaParentName != "" {
		location = joinUnique(firstNonEmpty(l.AreaName, l.District), l.AreaParentName)
	} else {
		location = firstNonEmpty(l.AreaName, l.District)
	}
	if location != "" {
		details = append(details, "📍 <b>"+html.EscapeString(location)+"</b>")
	}
	facts := []string{}
	if l.Rooms != nil {
		word := "rooms"
		if *l.Rooms == 1 {
			word = "room"
		}
		facts = append(facts, fmt.Sprintf("🚪 %d %s", *l.Rooms, word))
	}
	if l.AreaM2 != nil {
		facts = append(facts, "📐 "+formatFloat(*l.AreaM2)+" m²")
	}
	if len(facts) > 0 {
		details = append(details, strings.Join(facts, "  ·  "))
	}
	if l.Address != "" {
		details = append(details, "🛣 <b>Street:</b> "+html.EscapeString(l.Address))
	}
	if l.PropertyType == domain.PropertyApartment {
		if l.Floor != nil {
			floor := strconv.Itoa(*l.Floor)
			if l.TotalFloors != nil {
				floor += "/" + strconv.Itoa(*l.TotalFloors)
			}
			details = append(details, "🏬 <b>Floor:</b> "+floor)
		}
		if l.BuildingSeries != "" {
			details = append(details, "🏗 <b>Series:</b> "+html.EscapeString(l.BuildingSeries))
		}
		if l.BuildingType != "" {
			details = append(details, "🏠 <b>House type:</b> "+html.EscapeString(l.BuildingType))
		}
	} else {
		if l.TotalFloors != nil {
			details = append(details, fmt.Sprintf("🏗 <b>Floors:</b> %d", *l.TotalFloors))
		}
		if l.LandAreaM2 != nil {
			details = append(details, "🌳 <b>Land area:</b> "+formatArea(*l.LandAreaM2)+" m²")
		}
	}
	title := []rune(l.Title)
	if len(title) > 350 {
		title = title[:350]
	}
	source := providerName(l.Source)
	sections := []string{fmt.Sprintf("%s <b>%s %s · %s</b>", icon, property, action, price), strings.Join(details, "\n"), "<i>" + html.EscapeString(string(title)) + "</i>", `<a href="` + html.EscapeString(l.URL) + `">View on ` + html.EscapeString(source) + ` →</a>`}
	var nonempty []string
	for _, section := range sections {
		if section != "" && section != "<i></i>" {
			nonempty = append(nonempty, section)
		}
	}
	return strings.Join(nonempty, "\n\n")
}

func providerName(source string) string {
	switch source {
	case "ss.lv":
		return "SS.lv"
	case "city24.lv":
		return "City24.lv"
	default:
		return source
	}
}

func joinUnique(values ...string) string {
	var result []string
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return strings.Join(result, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func groupInt(value int) string {
	s := strconv.Itoa(value)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatArea(value float64) string {
	if value == float64(int64(value)) {
		return groupInt(int(value))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}
