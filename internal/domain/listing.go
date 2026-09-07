package domain

import "time"

type DealType string

const (
	DealRent DealType = "rent"
	DealSale DealType = "sale"
)

func (d DealType) Valid() bool {
	return d == DealRent || d == DealSale
}

type PropertyType string

const (
	PropertyApartment PropertyType = "apartment"
	PropertyHouse     PropertyType = "house"
)

func (p PropertyType) Valid() bool {
	return p == PropertyApartment || p == PropertyHouse
}

type ListingKey struct {
	Source, ExternalID string
}

type Listing struct {
	ID              int64
	Source          string
	ExternalID      string
	URL             string
	DealType        DealType
	PropertyType    PropertyType
	Title           string
	PriceEUR        *int
	Rooms           *int
	AreaM2          *float64
	City            string
	District        string
	Address         string
	Floor           *int
	TotalFloors     *int
	BuildingSeries  string
	BuildingType    string
	LandAreaM2      *float64
	DetailsEnriched bool
	SourceAreaKey   string
	SourceAreaName  string
	AreaKey         string
	AreaName        string
	AreaType        string
	AreaParentKey   string
	AreaParentName  string
	PublishedAt     *time.Time
	ImageURL        string
	PhotoURLs       []string
}

func (l Listing) Key() ListingKey {
	return ListingKey{Source: l.Source, ExternalID: l.ExternalID}
}

type AreaChoice struct{ Key, Label string }

type SearchFilter struct {
	ID            int64
	UserID        int64
	DealType      DealType
	PropertyTypes []PropertyType
	AreaKeys      []string
	PriceMin      *int
	PriceMax      *int
	RoomsMin      *int
	RoomsMax      *int
	AreaMin       *float64
	AreaMax       *float64
	Enabled       bool
	ActivatedAt   *time.Time
}

type SavedFilter struct {
	ID            int64
	DealType      DealType
	PropertyTypes []PropertyType
	AreaLabels    []string
	PriceMin      *int
	PriceMax      *int
	RoomsMin      *int
	RoomsMax      *int
	AreaMin       *float64
	AreaMax       *float64
	Enabled       bool
}

type PendingNotification struct {
	ID               int64
	TelegramUserID   int64
	UserName         string
	ChatID           int64
	LanguageTag      string
	FilterID         int64
	ListingID        int64
	Listing          Listing
	Attempts         int
	Type             NotificationType
	PreviousPriceEUR *int
	CurrentPriceEUR  *int
}

type NotificationType string

const (
	NotificationListingDiscovered NotificationType = "listing_discovered"
	NotificationPriceChanged      NotificationType = "price_changed"
)

type DiscoveryResult struct {
	Inserted int
	Events   int
}
