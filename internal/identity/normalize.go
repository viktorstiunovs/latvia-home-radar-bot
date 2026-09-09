package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"golang.org/x/text/unicode/norm"
)

const NormalizationVersion = "text-nfkd-v1"

var (
	htmlTags      = regexp.MustCompile(`(?s)<[^>]*>`)
	lineBreakTags = regexp.MustCompile(`(?i)<br\s*/?>`)
	emailToken    = regexp.MustCompile(`(?i)\b[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}\b`)
	urlToken      = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	phoneToken    = regexp.MustCompile(`(?:\+?\d[\d\s().-]{6,}\d)`)
	boilerplate   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:vairāk|papildu) informācijas pa (?:tālruni|telefonu)\b`),
		regexp.MustCompile(`(?i)\bmore information (?:by|via) (?:phone|telephone)\b`),
		regexp.MustCompile(`(?i)\b(?:подробности|больше информации) по телефону\b`),
		regexp.MustCompile(`(?i)\bstarpniekus lūdzam netraucēt\b`),
		regexp.MustCompile(`(?i)\bno agents please\b`),
		regexp.MustCompile(`(?i)\bпосредников просьба не беспокоить\b`),
	}
)

func NormalizeDescription(value string) string {
	value = plainText(value)
	value = emailToken.ReplaceAllString(value, " ")
	value = urlToken.ReplaceAllString(value, " ")
	value = phoneToken.ReplaceAllString(value, " ")
	for _, pattern := range boilerplate {
		value = pattern.ReplaceAllString(value, " ")
	}
	return canonicalText(value, true)
}

func NormalizeAddress(value string) string {
	value = canonicalText(plainText(value), true)
	words := strings.Fields(value)
	for index, word := range words {
		switch word {
		case "i", "iel", "iela", "street", "str", "st", "ulitsa", "ul", "улица", "ул":
			words[index] = "iela"
		case "bulvaris", "bulv", "boulevard", "blvd":
			words[index] = "bulvaris"
		case "prospekts", "prosp", "avenue", "ave":
			words[index] = "prospekts"
		}
	}
	return strings.Join(words, " ")
}

func SignalInputHash(listing domain.Listing, photoURLs []string) string {
	input := struct {
		Source                string
		ExternalID            string
		PropertyType          domain.PropertyType
		DealType              domain.DealType
		Description           string
		NormalizedAddress     string
		NormalizedDescription string
		AreaKey               string
		Rooms                 *int
		AreaM2                *float64
		Floor                 *int
		TotalFloors           *int
		BuildingSeries        string
		BuildingType          string
		LandAreaM2            *float64
		PhotoURLs             []string
	}{
		Source:                listing.Source,
		ExternalID:            listing.ExternalID,
		PropertyType:          listing.PropertyType,
		DealType:              listing.DealType,
		Description:           listing.Description,
		NormalizedAddress:     listing.NormalizedAddress,
		NormalizedDescription: listing.NormalizedDescription,
		AreaKey:               listing.AreaKey,
		Rooms:                 listing.Rooms,
		AreaM2:                listing.AreaM2,
		Floor:                 listing.Floor,
		TotalFloors:           listing.TotalFloors,
		BuildingSeries:        listing.BuildingSeries,
		BuildingType:          listing.BuildingType,
		LandAreaM2:            listing.LandAreaM2,
		PhotoURLs:             photoURLs,
	}
	encoded, _ := json.Marshal(input)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func plainText(value string) string {
	value = lineBreakTags.ReplaceAllString(value, " ")
	return html.UnescapeString(htmlTags.ReplaceAllString(value, " "))
}

func canonicalText(value string, foldMarks bool) string {
	value = strings.ToLower(norm.NFKD.String(value))
	var result strings.Builder
	space := true
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			if !foldMarks {
				result.WriteRune(r)
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			space = false
			continue
		}
		if !space {
			result.WriteByte(' ')
			space = true
		}
	}
	return norm.NFC.String(strings.TrimSpace(result.String()))
}
