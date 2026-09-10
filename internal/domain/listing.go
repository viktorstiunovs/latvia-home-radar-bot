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
	ID                    int64
	Source                string
	ExternalID            string
	URL                   string
	DealType              DealType
	PropertyType          PropertyType
	Title                 string
	Description           string
	NormalizedAddress     string
	NormalizedDescription string
	PriceEUR              *int
	Rooms                 *int
	AreaM2                *float64
	City                  string
	District              string
	Address               string
	Floor                 *int
	TotalFloors           *int
	BuildingSeries        string
	BuildingType          string
	LandAreaM2            *float64
	DetailsEnriched       bool
	SourceAreaKey         string
	SourceAreaName        string
	AreaKey               string
	AreaName              string
	AreaType              string
	AreaParentKey         string
	AreaParentName        string
	PublishedAt           *time.Time
	ImageURL              string
	PhotoURLs             []string
	AvailabilityStatus    AvailabilityStatus
	FirstSeenAt           time.Time
	LastSeenAt            time.Time
	AvailabilityChangedAt time.Time
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
	PropertyID       *int64
	ComparisonID     *int64
	Alternatives     []ListingAlternative
}

type NotificationType string

const (
	NotificationListingDiscovered NotificationType = "listing_discovered"
	NotificationPriceChanged      NotificationType = "price_changed"
	NotificationRelistingChanged  NotificationType = "relisting_price_changed"
	NotificationCheaperOffer      NotificationType = "cheaper_offer"
)

type ListingAlternative struct {
	ListingID int64  `json:"listing_id"`
	Source    string `json:"source"`
	URL       string `json:"url"`
	PriceEUR  *int   `json:"price_eur"`
}

type PropertyDiscoveryDecision struct {
	Notify              bool
	Type                NotificationType
	PreviousPriceEUR    *int
	ComparisonListingID *int64
}

func ClassifyPropertyDiscovery(currentPriceEUR *int, activeAlternatives []ListingAlternative, priorInactive *ListingAlternative, alreadyNotified bool) PropertyDiscoveryDecision {
	if len(activeAlternatives) > 0 {
		if !alreadyNotified {
			return PropertyDiscoveryDecision{Notify: true, Type: NotificationListingDiscovered}
		}
		var best *ListingAlternative
		for index := range activeAlternatives {
			candidate := &activeAlternatives[index]
			if candidate.PriceEUR != nil && (best == nil || *candidate.PriceEUR < *best.PriceEUR) {
				best = candidate
			}
		}
		if currentPriceEUR != nil && best != nil && *currentPriceEUR < *best.PriceEUR {
			comparisonID := best.ListingID
			return PropertyDiscoveryDecision{Notify: true, Type: NotificationCheaperOffer, PreviousPriceEUR: best.PriceEUR, ComparisonListingID: &comparisonID}
		}
		return PropertyDiscoveryDecision{}
	}
	if priorInactive != nil && currentPriceEUR != nil && priorInactive.PriceEUR != nil && *currentPriceEUR != *priorInactive.PriceEUR {
		comparisonID := priorInactive.ListingID
		return PropertyDiscoveryDecision{Notify: true, Type: NotificationRelistingChanged, PreviousPriceEUR: priorInactive.PriceEUR, ComparisonListingID: &comparisonID}
	}
	if alreadyNotified {
		return PropertyDiscoveryDecision{}
	}
	return PropertyDiscoveryDecision{Notify: true, Type: NotificationListingDiscovered}
}

type DiscoveryResult struct {
	Inserted int
	Events   int
}

type ListingSignalJob struct {
	Listing    Listing
	Generation int64
	AttemptID  int64
}

type PhotoFingerprint struct {
	Position            int
	SourceURL           string
	ExactHash           string
	ExactAlgorithm      string
	PerceptualHash      string
	PerceptualAlgorithm string
	MediaType           string
	ByteSize            int
	Width               int
	Height              int
}

type ListingSignals struct {
	Listing              Listing
	InputHash            string
	NormalizationVersion string
	Photos               []PhotoFingerprint
	Availability         *AvailabilityObservation
}

type AvailabilityStatus string

const (
	AvailabilityActive   AvailabilityStatus = "active"
	AvailabilityInactive AvailabilityStatus = "inactive"
	AvailabilityUnknown  AvailabilityStatus = "unknown"
)

func (s AvailabilityStatus) Valid() bool {
	return s == AvailabilityActive || s == AvailabilityInactive || s == AvailabilityUnknown
}

type AvailabilityObservation struct {
	Status   AvailabilityStatus
	Evidence string
}

type AvailabilityJob struct {
	Listing   Listing
	AttemptID int64
}

type DuplicateDecisionStatus string

const (
	DuplicateAccepted  DuplicateDecisionStatus = "accepted"
	DuplicateAmbiguous DuplicateDecisionStatus = "ambiguous"
	DuplicateRejected  DuplicateDecisionStatus = "rejected"
)

type ListingEvidence struct {
	SnapshotID  int64
	CompletedAt time.Time
	Listing     Listing
	Photos      []PhotoFingerprint
}

type DuplicateResolutionJob struct {
	Evidence  ListingEvidence
	AttemptID int64
}

type DuplicateEvidence struct {
	PhotoEvidence         string   `json:"photo_evidence"`
	ExactPhotoMatches     int      `json:"exact_photo_matches"`
	MinimumPhotoDistance  *int     `json:"minimum_photo_distance,omitempty"`
	AddressMatch          bool     `json:"address_match"`
	AreaMatch             bool     `json:"area_match"`
	DescriptionSimilarity float64  `json:"description_similarity"`
	CompatibleFacts       []string `json:"compatible_facts"`
	ConflictingFacts      []string `json:"conflicting_facts"`
}

type DuplicateDecision struct {
	Candidate   ListingEvidence
	RuleVersion string
	Confidence  float64
	Status      DuplicateDecisionStatus
	Evidence    DuplicateEvidence
}
