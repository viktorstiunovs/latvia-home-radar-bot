package sslv

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

var feedURLs = map[string]string{"apartment:rent": "https://www.ss.lv/lv/real-estate/flats/hand_over/rss/", "apartment:sale": "https://www.ss.lv/lv/real-estate/flats/sell/rss/", "house:rent": "https://www.ss.lv/lv/real-estate/homes-summer-residences/hand_over/rss/", "house:sale": "https://www.ss.lv/lv/real-estate/homes-summer-residences/sell/rss/"}
var photoPattern = regexp.MustCompile(`(?i)https://i\.ss\.lv/gallery/[^"'<>\s]+?\.800\.(?:jpe?g|png|webp)`)
var fieldPattern = regexp.MustCompile(`(?is)<td\b[^>]*class=["']?ads_opt_name["']?[^>]*>(.*?)</td>\s*<td\b[^>]*class=["']?ads_opt["']?[^>]*>(.*?)</td>`)
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

type Source struct {
	property domain.PropertyType
	deal     domain.DealType
	client   *http.Client
}

func New(property domain.PropertyType, deal domain.DealType, client *http.Client) *Source {
	return &Source{property: property, deal: deal, client: client}
}

func (s *Source) Key() string {
	return fmt.Sprintf("ss.lv:latvia:%ss:%s", s.property, s.deal)
}

func (s *Source) FetchRecent(ctx context.Context) ([]domain.Listing, error) {
	body, err := s.get(ctx, feedURLs[string(s.property)+":"+string(s.deal)])
	if err != nil {
		return nil, err
	}
	return ParseFeed(string(body), s.deal, s.property)
}

func (s *Source) Enrich(ctx context.Context, listing domain.Listing) (domain.Listing, error) {
	body, err := s.get(ctx, listing.URL)
	if err != nil {
		return listing, err
	}
	result := EnrichFromPage(listing, string(body))
	result.PhotoURLs = ParsePhotoURLs(string(body), 10)
	if len(result.PhotoURLs) == 0 && listing.ImageURL != "" {
		result.PhotoURLs = []string{listing.ImageURL}
	}
	return result, nil
}

func (s *Source) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("SS.lv HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}

type rss struct {
	Items []rssItem `xml:"channel>item"`
}
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func ParseFeed(text string, deal domain.DealType, property domain.PropertyType) ([]domain.Listing, error) {
	var feed rss
	if err := xml.Unmarshal([]byte(text), &feed); err != nil {
		return nil, fmt.Errorf("SS.lv returned invalid RSS XML: %w", err)
	}
	if len(feed.Items) == 0 {
		return nil, fmt.Errorf("SS.lv RSS feed contained no items")
	}
	var result []domain.Listing
	for _, item := range feed.Items {
		rawURL, title := strings.TrimSpace(item.Link), cleanText(item.Title)
		if rawURL == "" || title == "" {
			continue
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		if id == "" {
			continue
		}
		region, leaf, address := parseLocation(item.Description)
		area := domain.ResolveSSArea(rawURL, region, leaf)
		floor, totalFloors := parseFloorField(item.Description, "Stāvs")
		listing := domain.Listing{Source: "ss.lv", ExternalID: id, URL: rawURL, DealType: deal, PropertyType: property, Title: title, PriceEUR: parseIntField(item.Description, "Cena"), Rooms: parseIntField(item.Description, "Ist."), AreaM2: parseFloatField(item.Description, "m²"), Address: address, Floor: floor, TotalFloors: totalFloors, BuildingSeries: parseTextField(item.Description, "Sērija"), LandAreaM2: parseAreaField(item.Description, "Zem. pl."), PublishedAt: parseDate(item.PubDate), ImageURL: parseImageURL(item.Description)}
		if property == domain.PropertyApartment {
			listing.City = region
		} else {
			listing.TotalFloors = parseIntField(item.Description, "Stāvi")
		}
		if area != nil {
			listing.SourceAreaKey = area.SourceKey
			listing.SourceAreaName = area.RawName
			if area.Area != nil {
				listing.District = area.Area.Name
				listing.AreaKey = area.Area.Key
				listing.AreaName = area.Area.Name
				listing.AreaType = area.Area.Type
			}
			if area.Parent != nil {
				listing.AreaParentKey = area.Parent.Key
				listing.AreaParentName = area.Parent.Name
			}
		} else {
			listing.District = leaf
			if listing.District == "" {
				listing.District = region
			}
		}
		result = append(result, listing)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("SS.lv RSS items could not be parsed")
	}
	return result, nil
}

func EnrichFromPage(listing domain.Listing, text string) domain.Listing {
	fields := detailFields(text)
	if len(fields) == 0 {
		return listing
	}
	first := func(labels ...string) string {
		for _, label := range labels {
			if v := fields[strings.ToLower(label)]; v != "" {
				return v
			}
		}
		return ""
	}
	if v := first("Iela", "Street"); v != "" {
		listing.Address = v
	}
	if v := parsePlainInt(first("Istabas", "Rooms")); v != nil {
		listing.Rooms = v
	}
	if v := parseMeasurement(first("Platība", "Area")); v != nil {
		listing.AreaM2 = v
	}
	if listing.PropertyType == domain.PropertyApartment {
		if v := first("Pilsēta", "City"); v != "" {
			listing.City = v
		}
		if v := first("Rajons", "District"); v != "" {
			listing.District = v
		}
		floor, total := parseFloorValue(first("Stāvs", "Floor / floors"))
		if floor != nil {
			listing.Floor = floor
		}
		if total != nil {
			listing.TotalFloors = total
		}
		if v := first("Sērija", "Series"); v != "" {
			listing.BuildingSeries = v
		}
		if v := first("Mājas tips", "House type"); v != "" {
			listing.BuildingType = v
		}
	} else {
		if v := first("Pilsēta/pagasts", "City/civil parish"); v != "" {
			listing.City = v
		}
		if v := first("Pilsēta, rajons", "City, district"); v != "" {
			listing.District = v
		}
		if v := parsePlainInt(first("Stāvu skaits", "Amount of floors")); v != nil {
			listing.TotalFloors = v
		}
		if v := parseMeasurement(first("Zemes platība", "Land area")); v != nil {
			listing.LandAreaM2 = v
		}
	}
	listing.DetailsEnriched = true
	return listing
}

func ParsePhotoURLs(text string, limit int) []string {
	matches := photoPattern.FindAllString(html.UnescapeString(text), -1)
	seen := map[string]bool{}
	result := []string{}
	for _, item := range matches {
		if !seen[item] {
			result = append(result, item)
			seen[item] = true
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

func fieldHTML(description, label string) string {
	pattern := regexp.MustCompile(`(?is)` + regexp.QuoteMeta(label) + `:\s*<b>(.*?)</b>`)
	match := pattern.FindStringSubmatch(description)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func parseLocation(description string) (string, string, string) {
	if value := fieldHTML(description, "Rajons"); value != "" {
		lines := htmlLines(value)
		if len(lines) > 1 {
			return lines[0], "", lines[1]
		}
		if len(lines) > 0 {
			return lines[0], "", ""
		}
	}
	if value := fieldHTML(description, "Pagasts"); value != "" {
		lines := htmlLines(value)
		if len(lines) > 1 {
			return "", lines[0], lines[1]
		}
		if len(lines) > 0 {
			return "", lines[0], ""
		}
	}
	return "", "", ""
}

func parseIntField(description, label string) *int {
	plain := cleanHTML(fieldHTML(description, label))
	match := regexp.MustCompile(`[0-9][0-9\s,.]*`).FindString(plain)
	digits := regexp.MustCompile(`\D`).ReplaceAllString(match, "")
	if digits == "" {
		return nil
	}
	value, _ := strconv.Atoi(digits)
	return &value
}

func parseFloatField(description, label string) *float64 {
	plain := strings.ReplaceAll(cleanHTML(fieldHTML(description, label)), ",", ".")
	match := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`).FindString(plain)
	if match == "" {
		return nil
	}
	value, _ := strconv.ParseFloat(match, 64)
	return &value
}

func parseTextField(description, label string) string {
	return cleanHTML(fieldHTML(description, label))
}

func parseFloorField(description, label string) (*int, *int) {
	return parseFloorValue(cleanHTML(fieldHTML(description, label)))
}

func parseFloorValue(value string) (*int, *int) {
	match := regexp.MustCompile(`([0-9]+)\s*/\s*([0-9]+)`).FindStringSubmatch(value)
	if len(match) < 3 {
		return nil, nil
	}
	a, _ := strconv.Atoi(match[1])
	b, _ := strconv.Atoi(match[2])
	return &a, &b
}

func parseAreaField(description, label string) *float64 {
	return parseMeasurement(cleanHTML(fieldHTML(description, label)))
}

func parseMeasurement(value string) *float64 {
	match := regexp.MustCompile(`[0-9][0-9\s]*(?:[.,][0-9]+)?`).FindString(value)
	if match == "" {
		return nil
	}
	number, _ := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(match, " ", ""), ",", "."), 64)
	if regexp.MustCompile(`(?i)\bha\.?\b`).MatchString(value) {
		number *= 10000
	}
	return &number
}

func parsePlainInt(value string) *int {
	match := regexp.MustCompile(`[0-9]+`).FindString(value)
	if match == "" {
		return nil
	}
	number, _ := strconv.Atoi(match)
	return &number
}

func detailFields(text string) map[string]string {
	result := map[string]string{}
	document, err := goquery.NewDocumentFromReader(strings.NewReader(text))
	if err == nil {
		document.Find("td.ads_opt_name").Each(func(_ int, labelCell *goquery.Selection) {
			label := strings.TrimSuffix(strings.ToLower(cleanText(labelCell.Text())), ":")
			valueCell := labelCell.NextFiltered("td.ads_opt")
			value := cleanText(valueCell.Text())
			value = regexp.MustCompile(`(?i)\s*\[\s*(?:Karte|Map|Карта)\s*\]\s*`).ReplaceAllString(value, "")
			if label != "" && value != "" {
				result[label] = strings.TrimSpace(value)
			}
		})
	}
	if len(result) > 0 {
		return result
	}
	for _, match := range fieldPattern.FindAllStringSubmatch(text, -1) {
		label := strings.TrimSuffix(strings.ToLower(cleanHTML(match[1])), ":")
		value := cleanHTML(match[2])
		value = regexp.MustCompile(`(?i)\s*\[\s*(?:Karte|Map|Карта)\s*\]\s*`).ReplaceAllString(value, "")
		if label != "" && value != "" {
			result[label] = strings.TrimSpace(value)
		}
	}
	return result
}

func parseImageURL(description string) string {
	match := regexp.MustCompile(`(?i)<img\b[^>]*\bsrc=["']?([^"'\s>]+)`).FindStringSubmatch(description)
	if len(match) > 1 {
		return html.UnescapeString(match[1])
	}
	return ""
}

func parseDate(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC1123Z, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC1123, value)
	}
	if err != nil {
		return nil
	}
	return &parsed
}

func htmlLines(value string) []string {
	value = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(value, "\n")
	value = htmlTagPattern.ReplaceAllString(value, "")
	var result []string
	for _, line := range strings.Split(value, "\n") {
		if line = cleanText(html.UnescapeString(line)); line != "" {
			result = append(result, line)
		}
	}
	return result
}

func cleanHTML(value string) string {
	return cleanText(html.UnescapeString(htmlTagPattern.ReplaceAllString(value, "")))
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
