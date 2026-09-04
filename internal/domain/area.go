package domain

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type AreaDefinition struct{ Key, Name, Type, ParentKey string }
type AreaResolution struct {
	SourceKey, RawName string
	Area, Parent       *AreaDefinition
}

var rootAreaNames = map[string]string{
	"riga": "Rīga", "jurmala": "Jūrmala", "riga-region": "Rīgas rajons",
	"aizkraukle-and-reg": "Aizkraukle un rajons", "aluksne-and-reg": "Alūksne un rajons",
	"balvi-and-reg": "Balvi un rajons", "bauska-and-reg": "Bauska un rajons", "cesis-and-reg": "Cēsis un rajons",
	"daugavpils-and-reg": "Daugavpils un rajons", "dobele-and-reg": "Dobele un rajons", "gulbene-and-reg": "Gulbene un rajons",
	"jekabpils-and-reg": "Jēkabpils un rajons", "jelgava-and-reg": "Jelgava un rajons", "kraslava-and-reg": "Krāslava un rajons",
	"kuldiga-and-reg": "Kuldīga un rajons", "liepaja-and-reg": "Liepāja un rajons", "limbadzi-and-reg": "Limbaži un rajons",
	"ludza-and-reg": "Ludza un rajons", "madona-and-reg": "Madona un rajons", "ogre-and-reg": "Ogre un rajons",
	"preili-and-reg": "Preiļi un rajons", "rezekne-and-reg": "Rēzekne un rajons", "saldus-and-reg": "Saldus un rajons",
	"talsi-and-reg": "Talsi un rajons", "tukums-and-reg": "Tukums un rajons", "valka-and-reg": "Valka un rajons",
	"valmiera-and-reg": "Valmiera un rajons", "ventspils-and-reg": "Ventspils un rajons", "other": "Cits",
	"flats-abroad-latvia": "Ārpus Latvijas", "houses-abroad-latvia": "Ārpus Latvijas",
}

var rigaChildren = map[string]string{
	"centre": "Centrs", "agenskalns": "Āgenskalns", "aplokciems": "Aplokciems", "beberbeki": "Beberbeķi", "bergi": "Berģi", "bierini": "Bieriņi", "bolderaya": "Bolderāja", "breksi": "Brekši", "bulli": "Buļļi", "bukulti": "Bukulti", "chiekurkalns": "Čiekurkalns", "darzciems": "Dārzciems", "darzini": "Dārziņi", "daugavgriva": "Daugavgrīva", "dreilini": "Dreiliņi", "dzeguzhkalns": "Dzegužkalns (Dzirciems)", "grizinkalns": "Grīziņkalns", "ilguciems": "Iļģuciems", "imanta": "Imanta", "jaunciems": "Jaunciems", "jaunmilgravis": "Jaunmīlgrāvis", "yugla": "Jugla", "katlakalns": "Katlakalns", "kengarags": "Ķengarags", "kipsala": "Ķīpsala", "kleisti": "Kleisti", "kliversala": "Klīversala", "krasta-st-area": "Krasta rajons", "kundzinsala": "Kundziņsala", "lucavsala": "Lucavsala", "maskavas-priekshpilseta": "Latgales priekšpilsēta", "mangali": "Mangaļi", "mangalsala": "Mangaļsala", "mezhapark": "Mežaparks", "mezhciems": "Mežciems", "plyavnieki": "Pļavnieki", "purvciems": "Purvciems", "rumbula": "Rumbula", "shampeteris-pleskodale": "Šampēteris-Pleskodāle", "sarkandaugava": "Sarkandaugava", "shkirotava": "Šķirotava", "teika": "Teika", "tornjakalns": "Torņakalns", "trisciems": "Trīsciems", "vecaki": "Vecāķi", "vecdaugava": "Vecdaugava", "vecmilgravis": "Vecmīlgrāvis", "vecriga": "Vecrīga", "voleri": "Voleri", "zakusala": "Zaķusala", "zasulauks": "Zasulauks", "ziepniekkalns": "Ziepniekkalns", "zolitude": "Zolitūde", "vef": "VEF", "other": "Cits",
}
var jurmalaChildren = map[string]string{"asari": "Asari", "bulduri": "Bulduri", "bulluciems": "Buļļuciems", "dubulti": "Dubulti", "dzintari": "Dzintari", "jaundubulti": "Jaundubulti", "jaunkemeri": "Jaunķemeri", "kauguri": "Kauguri", "kemeri": "Ķemeri", "lielupe": "Lielupe", "majori": "Majori", "melluzi": "Melluži", "priedaine": "Priedaine", "pumpuri": "Pumpuri", "sloka": "Sloka", "vaivari": "Vaivari", "valteri": "Valteri", "other": "Cits"}
var rigaRegionChildren = map[string]string{"baldone": "Baldone", "balozi": "Baloži", "olaine": "Olaine", "salaspils": "Salaspils", "saulkrasti": "Saulkrasti", "sigulda": "Sigulda", "vangazi": "Vangaži", "adazu-nov": "Ādažu nov.", "allazu-pag": "Allažu pag.", "babites-pag": "Babītes pag.", "baldones-l-t": "Baldones l. t.", "carnikavas-nov": "Carnikavas nov.", "daugmales-pag": "Daugmales pag.", "garkalnes-nov": "Garkalnes nov.", "incukalna-nov": "Inčukalna nov.", "kekavas-pag": "Ķekavas pag.", "krimuldas-pag": "Krimuldas pag.", "malpils-pag": "Mālpils pag.", "marupes-pag": "Mārupes pag.", "olaines-pag": "Olaines pag.", "ropazu-nov": "Ropažu nov.", "salas-pag": "Salas pag.", "salaspils-l-t": "Salaspils l. t.", "saulkrastu-l-t": "Saulkrastu l. t.", "sejas-nov": "Sējas nov.", "siguldas-pag": "Siguldas pag.", "stopinu-nov": "Stopiņu nov.", "other": "Cits"}
var childMaps = map[string]map[string]string{"riga": rigaChildren, "jurmala": jurmalaChildren, "riga-region": rigaRegionChildren}
var extraChildren = map[string][]string{"riga": {"Atgāzene", "Avoti", "Bišumuiža", "Biķernieki", "Brasa", "Mīlgrāvis", "Mūkusala", "Pārdaugava", "Pētersala", "Skanste", "Spilve", "Suži"}, "jurmala": {"Druvciems", "Kaugurciems"}, "riga-region": {"Ķekavas nov.", "Mārupes nov.", "Olaines nov.", "Salaspils nov.", "Saulkrastu nov.", "Siguldas nov.", "Ķīšupe"}}
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func NormalizeAreaName(value string) string {
	value = strings.ToLower(value)
	value = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(value))
	return strings.Join(strings.Fields(nonAlnum.ReplaceAllString(value, " ")), " ")
}

func CanonicalSlug(value string) string {
	return strings.ReplaceAll(NormalizeAreaName(value), " ", "-")
}

func RootAreaKey(slug, name string) string {
	if slug == "riga" {
		return "lv/riga"
	}
	if slug == "jurmala" {
		return "lv/jurmala"
	}
	if strings.Contains(slug, "abroad-latvia") {
		return "outside-latvia"
	}
	return "lv/" + CanonicalSlug(name)
}

func RootAreaName(slug string) string {
	return rootAreaNames[slug]
}

func AreaDefinitions() []AreaDefinition {
	defs := make([]AreaDefinition, 0, len(rootAreaNames)+100)
	rootSlugs := make([]string, 0, len(rootAreaNames))
	for slug := range rootAreaNames {
		rootSlugs = append(rootSlugs, slug)
	}
	sort.Strings(rootSlugs)
	for _, slug := range rootSlugs {
		name := rootAreaNames[slug]
		typ := "region"
		if slug == "riga" || slug == "jurmala" {
			typ = "city"
		}
		defs = append(defs, AreaDefinition{Key: RootAreaKey(slug, name), Name: name, Type: typ})
	}
	for _, slug := range []string{"riga", "jurmala", "riga-region"} {
		names := map[string]bool{}
		for _, name := range childMaps[slug] {
			names[name] = true
		}
		for _, name := range extraChildren[slug] {
			names[name] = true
		}
		ordered := make([]string, 0, len(names))
		for name := range names {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		parent := RootAreaKey(slug, rootAreaNames[slug])
		typ := "neighbourhood"
		if slug == "riga-region" {
			typ = "locality"
		}
		for _, name := range ordered {
			defs = append(defs, AreaDefinition{Key: parent + "/" + CanonicalSlug(name), Name: name, Type: typ, ParentKey: parent})
		}
	}
	return defs
}

func ResolveSSArea(rawURL, rawRegion, rawLeaf string) *AreaResolution {
	sourceKey := SSAreaKeyFromURL(rawURL)
	if sourceKey == "" {
		return nil
	}
	parts := strings.Split(sourceKey, "/")
	slug := parts[0]
	name := rootAreaNames[slug]
	if name == "" {
		name = rawRegion
	}
	if name == "" {
		return &AreaResolution{SourceKey: sourceKey, RawName: rawLeaf}
	}
	typ := "region"
	if slug == "riga" || slug == "jurmala" {
		typ = "city"
	}
	root := &AreaDefinition{Key: RootAreaKey(slug, name), Name: name, Type: typ}
	children, ok := childMaps[slug]
	if !ok || len(parts) < 2 {
		raw := rawLeaf
		if raw == "" {
			raw = rawRegion
		}
		return &AreaResolution{SourceKey: sourceKey, RawName: raw, Area: root}
	}
	leafName := children[parts[1]]
	if leafName == "" {
		leafName = rawLeaf
	}
	if leafName == "" {
		leafName = strings.Title(strings.ReplaceAll(parts[1], "-", " "))
	}
	childType := "neighbourhood"
	if slug == "riga-region" {
		childType = "locality"
	}
	leaf := &AreaDefinition{Key: root.Key + "/" + CanonicalSlug(leafName), Name: leafName, Type: childType, ParentKey: root.Key}
	raw := rawLeaf
	if raw == "" {
		raw = leafName
	}
	return &AreaResolution{SourceKey: sourceKey, RawName: raw, Area: leaf, Parent: root}
}

func SSAreaKeyFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(path.Clean(u.Path), "/"), "/")
	idx := -1
	for i, p := range parts {
		if p == "flats" || p == "homes-summer-residences" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(parts)-1 {
		return ""
	}
	return strings.Join(parts[idx+1:len(parts)-1], "/")
}
