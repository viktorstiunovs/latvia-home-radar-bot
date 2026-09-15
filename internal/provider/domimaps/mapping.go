package domimaps

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

const maxPhotoURLs = 10

var (
	roomsPattern      = regexp.MustCompile(`(?i)^\s*(\d+)\s+istab`)
	areaPattern       = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*m\^?2\^?`)
	floorPattern      = regexp.MustCompile(`(?i)^\s*(\d+)\s*/\s*(\d+)\s*st\.?`)
	totalFloorPattern = regexp.MustCompile(`(?i)^\s*(\d+)\s*st\.?`)
)

type summary struct {
	Price      string   `json:"p"`
	Heading    string   `json:"h"`
	Address    string   `json:"a"`
	Facts      []string `json:"e"`
	PhotoCount int      `json:"c"`
	Image      string   `json:"i"`
	Published  string   `json:"d"`
}

func listingFromSummary(id string, value summary, deal domain.DealType, property domain.PropertyType) (domain.Listing, error) {
	if id == "" {
		return domain.Listing{}, fmt.Errorf("DOMImaps listing has no id")
	}
	title := strings.TrimSpace(html.UnescapeString(value.Heading))
	location := strings.TrimSpace(html.UnescapeString(value.Address))
	city, district, address := locationParts(location)
	area, landArea, floor, totalFloors := facts(value.Facts, property)
	photos := photoURLs(id, value.PhotoCount)
	image := ""
	if len(photos) > 0 {
		image = photos[0]
	}

	return domain.Listing{
		Source:       "domimaps.lv",
		ExternalID:   id,
		URL:          "https://ad.domimaps.lv/" + id,
		DealType:     deal,
		PropertyType: property,
		Title:        title,
		PriceEUR:     priceEUR(value.Price),
		Rooms:        roomCount(title),
		AreaM2:       area,
		City:         city,
		District:     district,
		Address:      address,
		Floor:        floor,
		TotalFloors:  totalFloors,
		LandAreaM2:   landArea,
		PublishedAt:  publishedAt(value.Published),
		ImageURL:     image,
		PhotoURLs:    photos,
	}, nil
}

func priceEUR(value string) *int {
	upper := strings.ToUpper(html.UnescapeString(value))
	if !strings.Contains(upper, "EUR") {
		return nil
	}
	index := strings.Index(upper, "EUR")
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}

		return -1
	}, upper[:index])
	if digits == "" {
		return nil
	}
	amount, err := strconv.Atoi(digits)
	if err != nil {
		return nil
	}

	return &amount
}

func roomCount(title string) *int {
	match := roomsPattern.FindStringSubmatch(title)
	if len(match) != 2 {
		return nil
	}
	rooms, err := strconv.Atoi(match[1])
	if err != nil {
		return nil
	}

	return &rooms
}

func facts(values []string, property domain.PropertyType) (*float64, *float64, *int, *int) {
	var area *float64
	var landArea *float64
	var floor *int
	var totalFloors *int
	for _, raw := range values {
		value := strings.TrimSpace(html.UnescapeString(raw))
		if match := areaPattern.FindStringSubmatch(value); len(match) == 2 {
			parsed, err := parseLatvianNumber(match[1])
			if err == nil {
				if strings.Contains(strings.ToLower(value), "zeme") {
					landArea = &parsed
				} else if area == nil {
					area = &parsed
				}
			}
		}
		if match := floorPattern.FindStringSubmatch(value); len(match) == 3 {
			current, currentErr := strconv.Atoi(match[1])
			total, totalErr := strconv.Atoi(match[2])
			if currentErr == nil && totalErr == nil {
				floor = &current
				totalFloors = &total
			}
			continue
		}
		if property == domain.PropertyHouse {
			if match := totalFloorPattern.FindStringSubmatch(value); len(match) == 2 {
				total, err := strconv.Atoi(match[1])
				if err == nil {
					totalFloors = &total
				}
			}
		}
	}

	return area, landArea, floor, totalFloors
}

func parseLatvianNumber(value string) (float64, error) {
	if strings.Contains(value, ",") {
		value = strings.ReplaceAll(value, ".", "")
		value = strings.ReplaceAll(value, ",", ".")
	} else {
		parts := strings.Split(value, ".")
		thousands := len(parts) > 1
		for _, part := range parts[1:] {
			if len(part) != 3 {
				thousands = false
				break
			}
		}
		if thousands {
			value = strings.Join(parts, "")
		}
	}

	return strconv.ParseFloat(value, 64)
}

func locationParts(value string) (string, string, string) {
	rawParts := strings.Split(value, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	switch len(parts) {
	case 0:
		return "", "", ""
	case 1:
		return "", "", parts[0]
	case 2:
		return parts[0], "", parts[1]
	default:
		return parts[0], strings.Join(parts[1:len(parts)-1], ", "), parts[len(parts)-1]
	}
}

func publishedAt(value string) *time.Time {
	if value == "" {
		return nil
	}
	location, err := time.LoadLocation("Europe/Riga")
	if err != nil {
		return nil
	}
	parsed, err := time.ParseInLocation("02.01.2006", value, location)
	if err != nil {
		return nil
	}

	return &parsed
}

func photoURLs(id string, count int) []string {
	if count <= 0 {
		return nil
	}
	if count > maxPhotoURLs {
		count = maxPhotoURLs
	}
	result := make([]string, 0, count)
	for index := 0; index < count; index++ {
		result = append(result, fmt.Sprintf("https://adr.domimaps.lv/get_image/m-umap/%s_%d.jpg", id, index))
	}

	return result
}
