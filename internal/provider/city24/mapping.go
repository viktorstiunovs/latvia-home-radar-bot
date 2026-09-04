package city24

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

var rootSlugByID = map[int]string{245396: "riga", 245372: "jurmala", 245343: "riga-region", 245344: "riga-region", 245326: "riga-region", 245328: "riga-region", 245330: "riga-region", 245332: "riga-region", 245334: "riga-region", 245335: "riga-region", 245309: "aizkraukle-and-reg", 245310: "aluksne-and-reg", 245311: "daugavpils-and-reg", 245361: "daugavpils-and-reg", 245312: "balvi-and-reg", 245313: "bauska-and-reg", 245314: "cesis-and-reg", 245315: "liepaja-and-reg", 245379: "liepaja-and-reg", 245316: "dobele-and-reg", 245317: "gulbene-and-reg", 245318: "jelgava-and-reg", 245370: "jelgava-and-reg", 245319: "jekabpils-and-reg", 245320: "kraslava-and-reg", 245321: "kuldiga-and-reg", 245322: "limbadzi-and-reg", 245323: "ludza-and-reg", 245324: "preili-and-reg", 245325: "madona-and-reg", 245327: "ogre-and-reg", 245329: "preili-and-reg", 245331: "rezekne-and-reg", 245395: "rezekne-and-reg", 245333: "saldus-and-reg", 245336: "valka-and-reg", 245337: "talsi-and-reg", 245338: "tukums-and-reg", 245339: "valka-and-reg", 245340: "valmiera-and-reg", 245341: "madona-and-reg", 245342: "ventspils-and-reg", 245418: "ventspils-and-reg", 278550: "riga", 278551: "riga-region"}
var standaloneNames = map[int]string{278550: "Biķernieki", 278551: "Ķīšupe"}
var areaNameAliases = map[int]string{270705: "Šampēteris-Pleskodāle", 270742: "Latgales priekšpilsēta", 270739: "Krasta rajons", 270735: "Dzegužkalns (Dzirciems)", 270744: "Mangaļsala"}
var tagPattern = regexp.MustCompile(`<[^>]+>`)

func ParseOffer(payload map[string]any, deal domain.DealType, property domain.PropertyType) (domain.Listing, error) {
	id := stringValue(payload["id"])
	friendly := stringValue(payload["friendly_id"])
	if id == "" || friendly == "" {
		return domain.Listing{}, fmt.Errorf("City24 listing has no id or friendly_id")
	}
	if stringValue(payload["transaction_type"]) != transactionType(deal) {
		return domain.Listing{}, fmt.Errorf("unexpected City24 transaction type for %s", id)
	}
	if stringValue(payload["unit_type"]) != unitType(property) {
		return domain.Listing{}, fmt.Errorf("unexpected City24 property type for %s", id)
	}
	address := mapping(payload["address"])
	area := ResolveArea(address)
	attributes := mapping(payload["attributes"])
	photos := photoURLs(payload)
	image := ""
	if len(photos) > 0 {
		image = photos[0]
	} else {
		image = imageURL(payload["main_image"])
	}
	city, district := locationLabels(address)
	l := domain.Listing{Source: "city24.lv", ExternalID: id, URL: publicURL + friendly, DealType: deal, PropertyType: property, Title: listingTitle(payload, property, city, district), PriceEUR: intPtr(payload["price"]), Rooms: intPtr(payload["room_count"]), AreaM2: floatPtr(payload["property_size"]), City: city, District: district, Address: streetAddress(address), Floor: intPtr(attributes["FLOOR"]), TotalFloors: intPtr(attributes["TOTAL_FLOORS"]), BuildingSeries: attributeLabel(attributes["HOUSE_TYPE"]), BuildingType: attributeLabel(attributes["BUILDING_MATERIAL"]), LandAreaM2: landArea(payload), PublishedAt: timestamp(payload["date_created"]), ImageURL: image, PhotoURLs: photos}
	if area != nil {
		l.SourceAreaKey = area.SourceKey
		l.SourceAreaName = area.RawName
		if area.Area != nil {
			l.AreaKey = area.Area.Key
			l.AreaName = area.Area.Name
			l.AreaType = area.Area.Type
		}
		if area.Parent != nil {
			l.AreaParentKey = area.Parent.Key
			l.AreaParentName = area.Parent.Name
		}
	}
	return l, nil
}

func ResolveArea(address map[string]any) *domain.AreaResolution {
	county, city, district, parish, village := mapping(address["county"]), mapping(address["city"]), mapping(address["district"]), mapping(address["parish"]), mapping(address["village"])
	root := county
	if len(root) == 0 {
		root = city
	}
	rootID := intValue(root["id"])
	slug := rootSlugByID[rootID]
	if slug == "" {
		for _, entity := range []map[string]any{district, village, parish} {
			if candidate := rootSlugByID[intValue(entity["parent"])]; candidate != "" {
				slug = candidate
				break
			}
		}
	}
	if slug == "" {
		return nil
	}
	rootName := domain.RootAreaName(slug)
	typ := "region"
	if slug == "riga" || slug == "jurmala" {
		typ = "city"
	}
	rootDef := &domain.AreaDefinition{Key: domain.RootAreaKey(slug, rootName), Name: rootName, Type: typ}
	leaf := district
	if len(leaf) == 0 {
		leaf = village
	}
	if len(leaf) == 0 {
		leaf = parish
	}
	if len(leaf) == 0 && standaloneNames[rootID] != "" {
		leaf = root
	}
	if len(leaf) == 0 && len(county) > 0 && slug == "riga-region" {
		leaf = county
	}
	if len(leaf) == 0 && len(city) > 0 && intValue(city["id"]) != rootID {
		leaf = city
	}
	if len(leaf) == 0 {
		return &domain.AreaResolution{SourceKey: strconv.Itoa(rootID), RawName: stringValue(root["name"]), Area: rootDef}
	}
	leafID := intValue(leaf["id"])
	raw := stringValue(leaf["name"])
	if leafID == 0 || raw == "" {
		return &domain.AreaResolution{SourceKey: strconv.Itoa(rootID), RawName: raw, Area: rootDef}
	}
	name := areaNameAliases[leafID]
	if name == "" {
		name = standaloneNames[leafID]
	}
	if name == "" {
		name = raw
	}
	childType := "locality"
	if slug == "riga" || slug == "jurmala" {
		childType = "neighbourhood"
	}
	child := &domain.AreaDefinition{Key: rootDef.Key + "/" + domain.CanonicalSlug(name), Name: name, Type: childType, ParentKey: rootDef.Key}
	return &domain.AreaResolution{SourceKey: strconv.Itoa(leafID), RawName: raw, Area: child, Parent: rootDef}
}

func mapping(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func floatValue(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case jsonNumber:
		result, err := strconv.ParseFloat(string(v), 64)
		return result, err == nil
	}
	result, err := strconv.ParseFloat(stringValue(value), 64)
	return result, err == nil
}

type jsonNumber string

func intValue(value any) int {
	number, ok := floatValue(value)
	if !ok {
		return 0
	}
	return int(number)
}

func intPtr(value any) *int {
	number, ok := floatValue(value)
	if !ok {
		return nil
	}
	v := int(number)
	return &v
}

func floatPtr(value any) *float64 {
	number, ok := floatValue(value)
	if !ok {
		return nil
	}
	return &number
}

func imageURL(value any) string {
	url := stringValue(mapping(value)["url"])
	return strings.ReplaceAll(url, "{fmt:em}", "26")
}

func photoURLs(payload map[string]any) []string {
	images, ok := payload["images"].([]any)
	if !ok {
		return nil
	}
	result := []string{}
	seen := map[string]bool{}
	for _, item := range images {
		url := imageURL(item)
		if url != "" && !seen[url] {
			result = append(result, url)
			seen[url] = true
		}
		if len(result) == 10 {
			break
		}
	}
	return result
}

func entityName(address map[string]any, key string) string {
	return stringValue(mapping(address[key])["name"])
}

func locationLabels(address map[string]any) (string, string) {
	city := entityName(address, "city")
	if city == "" {
		city = entityName(address, "village")
	}
	county := entityName(address, "county")
	parish := entityName(address, "parish")
	district := entityName(address, "district")
	if district != "" {
		return city, district
	}
	if city != "" && county != "" {
		return city, county
	}
	if city == "" {
		city = parish
		if city == "" {
			city = county
		}
	}
	if parish != "" && county != "" {
		return city, county
	}
	return city, ""
}

func streetAddress(address map[string]any) string {
	street := entityName(address, "street")
	if street == "" {
		street = stringValue(address["street_name"])
	}
	number := ""
	if value, ok := address["export_house_number"].(bool); ok && value {
		number = stringValue(address["house_number"])
	}
	return strings.TrimSpace(street + " " + number)
}

func attributeLabel(value any) string {
	if list, ok := value.([]any); ok {
		if len(list) == 0 {
			return ""
		}
		value = list[0]
	}
	text := stringValue(value)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "_", " ")
	return strings.ToUpper(text[:1]) + strings.ToLower(text[1:])
}

func landArea(payload map[string]any) *float64 {
	value, ok := floatValue(payload["lot_size"])
	if !ok {
		return nil
	}
	if intValue(payload["lot_size_unit_id"]) == 5 {
		value *= 10000
	}
	return &value
}

func timestamp(value any) *time.Time {
	seconds, ok := floatValue(value)
	if !ok {
		return nil
	}
	parsed := time.Unix(int64(seconds), 0).UTC()
	return &parsed
}

func plainText(value any) string {
	text := stringValue(value)
	text = tagPattern.ReplaceAllString(html.UnescapeString(text), " ")
	return strings.Join(strings.Fields(text), " ")
}

func listingTitle(payload map[string]any, property domain.PropertyType, city, district string) string {
	descriptions := mapping(payload["descriptions"])
	for _, locale := range []string{"lv_LV", "ru_RU", "en_GB", "en_US"} {
		localized := mapping(descriptions[locale])
		for _, field := range []string{"introduction", "description"} {
			if text := plainText(localized[field]); text != "" {
				runes := []rune(text)
				if len(runes) > 500 {
					runes = runes[:500]
				}
				return string(runes)
			}
		}
	}
	name := "Apartment"
	if property == domain.PropertyHouse {
		name = "House"
	}
	location := district
	if location == "" {
		location = city
	}
	if location != "" {
		return name + " in " + location
	}
	return name
}
