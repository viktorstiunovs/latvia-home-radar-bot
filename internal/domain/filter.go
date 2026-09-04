package domain

import "strings"

func Matches(listing Listing, filter SearchFilter) bool {
	if listing.DealType != filter.DealType || !containsProperty(filter.PropertyTypes, listing.PropertyType) {
		return false
	}
	if len(filter.AreaKeys) > 0 {
		matched := false
		for _, selected := range filter.AreaKeys {
			if listing.AreaKey == selected || strings.HasPrefix(listing.AreaKey, selected+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return intInRange(listing.PriceEUR, filter.PriceMin, filter.PriceMax) &&
		intInRange(listing.Rooms, filter.RoomsMin, filter.RoomsMax) &&
		floatInRange(listing.AreaM2, filter.AreaMin, filter.AreaMax)
}

func containsProperty(types []PropertyType, candidate PropertyType) bool {
	if len(types) == 0 {
		types = []PropertyType{PropertyApartment}
	}
	for _, value := range types {
		if value == candidate {
			return true
		}
	}
	return false
}

func intInRange(value, minimum, maximum *int) bool {
	if minimum != nil && (value == nil || *value < *minimum) {
		return false
	}
	if maximum != nil && (value == nil || *value > *maximum) {
		return false
	}
	return true
}

func floatInRange(value, minimum, maximum *float64) bool {
	if minimum != nil && (value == nil || *value < *minimum) {
		return false
	}
	if maximum != nil && (value == nil || *value > *maximum) {
		return false
	}
	return true
}
