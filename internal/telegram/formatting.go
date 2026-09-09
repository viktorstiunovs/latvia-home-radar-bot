package telegram

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
)

func FormatListing(localizer localization.Localizer, l domain.Listing) string {
	return formatListing(localizer, l, "", nil)
}

func FormatListingWithAlternatives(localizer localization.Localizer, l domain.Listing, alternatives []domain.ListingAlternative) string {
	return formatListing(localizer, l, "", alternatives)
}

func FormatPriceChange(localizer localization.Localizer, l domain.Listing, previousPriceEUR, currentPriceEUR *int) string {
	l.PriceEUR = currentPriceEUR
	previous := formatListingPrice(localizer, l.DealType, previousPriceEUR)
	current := formatListingPrice(localizer, l.DealType, currentPriceEUR)
	priceChange := localizer.Text(localization.ListingPriceChangeHeading, nil) + "\n<blockquote>" +
		localizer.Text(localization.ListingPriceTransition, map[string]any{"Previous": previous, "Current": current})
	if previousPriceEUR != nil && currentPriceEUR != nil && *previousPriceEUR > 0 && *currentPriceEUR >= 0 && *currentPriceEUR != *previousPriceEUR {
		percentage := float64(*currentPriceEUR-*previousPriceEUR) * 100 / float64(*previousPriceEUR)
		sign := "+"
		heading := localization.ListingPriceIncreasedHeading
		if percentage < 0 {
			sign = "−"
			percentage = -percentage
			heading = localization.ListingPriceDecreasedHeading
		}
		formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(percentage, 'f', 1, 64), "0"), ".")
		priceChange = localizer.Text(heading, map[string]any{"Percentage": sign + formatted + "%"}) + "\n<blockquote>" +
			localizer.Text(localization.ListingPriceTransition, map[string]any{"Previous": previous, "Current": current})
	}
	priceChange += "</blockquote>"
	return formatListing(localizer, l, priceChange, nil)
}

func FormatCheaperOffer(localizer localization.Localizer, l domain.Listing, previousPriceEUR, currentPriceEUR *int, alternatives []domain.ListingAlternative) string {
	l.PriceEUR = currentPriceEUR
	previous := formatListingPrice(localizer, l.DealType, previousPriceEUR)
	current := formatListingPrice(localizer, l.DealType, currentPriceEUR)
	percentage := ""
	if previousPriceEUR != nil && currentPriceEUR != nil && *previousPriceEUR > 0 {
		change := float64(*previousPriceEUR-*currentPriceEUR) * 100 / float64(*previousPriceEUR)
		formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(change, 'f', 1, 64), "0"), ".")
		percentage = "−" + formatted + "%"
	}
	heading := localizer.Text(localization.ListingCheaperOfferHeading, map[string]any{"Percentage": percentage})
	transition := localizer.Text(localization.ListingPriceTransition, map[string]any{"Previous": previous, "Current": current})
	return formatListing(localizer, l, heading+"\n<blockquote>"+transition+"</blockquote>", alternatives)
}

func formatListing(localizer localization.Localizer, l domain.Listing, priceChange string, alternatives []domain.ListingAlternative) string {
	property, icon := localizer.Text(localization.ListingApartment, nil), "🏢"
	if l.PropertyType == domain.PropertyHouse {
		property, icon = localizer.Text(localization.ListingHouse, nil), "🏡"
	}
	action := localizer.Text(localization.ListingForRent, nil)
	if l.DealType == domain.DealSale {
		action = localizer.Text(localization.ListingForSale, nil)
	}
	price := formatListingPrice(localizer, l.DealType, l.PriceEUR)
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
		data := map[string]any{"Count": *l.Rooms}
		facts = append(facts, "🚪 "+localizer.Plural(localization.ListingRooms, *l.Rooms, data))
	}
	if l.AreaM2 != nil {
		facts = append(facts, "📐 "+formatFloat(*l.AreaM2)+" m²")
	}
	if len(facts) > 0 {
		details = append(details, strings.Join(facts, "  ·  "))
	}
	if l.Address != "" {
		details = append(details, localizer.Text(localization.ListingStreet, map[string]any{"Value": html.EscapeString(l.Address)}))
	}
	if l.PropertyType == domain.PropertyApartment {
		if l.Floor != nil {
			floor := strconv.Itoa(*l.Floor)
			if l.TotalFloors != nil {
				floor += "/" + strconv.Itoa(*l.TotalFloors)
			}
			details = append(details, localizer.Text(localization.ListingFloor, map[string]any{"Value": floor}))
		}
		if l.BuildingSeries != "" {
			details = append(details, localizer.Text(localization.ListingSeries, map[string]any{"Value": html.EscapeString(l.BuildingSeries)}))
		}
		if l.BuildingType != "" {
			details = append(details, localizer.Text(localization.ListingHouseType, map[string]any{"Value": html.EscapeString(l.BuildingType)}))
		}
	} else {
		if l.TotalFloors != nil {
			details = append(details, localizer.Text(localization.ListingFloors, map[string]any{"Count": *l.TotalFloors}))
		}
		if l.LandAreaM2 != nil {
			details = append(details, localizer.Text(localization.ListingLandArea, map[string]any{"Value": formatArea(*l.LandAreaM2)}))
		}
	}
	title := []rune(l.Title)
	if len(title) > 350 {
		title = title[:350]
	}
	source := providerName(l.Source)
	heading := localizer.Text(localization.ListingHeading, map[string]any{"Icon": icon, "Property": property, "Action": action, "Price": price})
	link := localizer.Text(localization.ListingViewOn, map[string]any{"URL": html.EscapeString(l.URL), "Source": html.EscapeString(source)})
	sections := []string{heading, priceChange, strings.Join(details, "\n"), "<i>" + html.EscapeString(string(title)) + "</i>", link, formatAlternatives(localizer, l.DealType, alternatives)}
	var nonempty []string
	for _, section := range sections {
		if section != "" && section != "<i></i>" {
			nonempty = append(nonempty, section)
		}
	}
	return strings.Join(nonempty, "\n\n")
}

func formatAlternatives(localizer localization.Localizer, deal domain.DealType, alternatives []domain.ListingAlternative) string {
	if len(alternatives) == 0 {
		return ""
	}
	lines := []string{localizer.Text(localization.ListingOtherOffersHeading, nil)}
	for index, alternative := range alternatives {
		if index == 3 {
			break
		}
		lines = append(lines, localizer.Text(localization.ListingOtherOffer, map[string]any{
			"URL":    html.EscapeString(alternative.URL),
			"Source": html.EscapeString(providerName(alternative.Source)),
			"Price":  formatListingPrice(localizer, deal, alternative.PriceEUR),
		}))
	}
	return strings.Join(lines, "\n")
}

func formatListingPrice(localizer localization.Localizer, deal domain.DealType, priceEUR *int) string {
	if priceEUR == nil {
		return localizer.Text(localization.ListingPriceOnRequest, nil)
	}
	price := "€" + groupInt(*priceEUR)
	if deal == domain.DealRent {
		return localizer.Text(localization.ListingPerMonth, map[string]any{"Price": price})
	}
	return price
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
