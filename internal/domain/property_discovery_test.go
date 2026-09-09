package domain

import "testing"

func TestClassifyPropertyDiscovery(t *testing.T) {
	tests := []struct {
		name      string
		current   *int
		active    []ListingAlternative
		inactive  *ListingAlternative
		notified  bool
		wantType  NotificationType
		wantAlert bool
	}{
		{"new property", integerPointer(700), nil, nil, false, NotificationListingDiscovered, true},
		{"duplicate suppressed", integerPointer(700), []ListingAlternative{{ListingID: 1, PriceEUR: integerPointer(650)}}, nil, true, "", false},
		{"cheaper concurrent offer", integerPointer(600), []ListingAlternative{{ListingID: 1, PriceEUR: integerPointer(650)}}, nil, true, NotificationCheaperOffer, true},
		{"equal concurrent offer", integerPointer(650), []ListingAlternative{{ListingID: 1, PriceEUR: integerPointer(650)}}, nil, true, "", false},
		{"higher concurrent offer", integerPointer(700), []ListingAlternative{{ListingID: 1, PriceEUR: integerPointer(650)}}, nil, true, "", false},
		{"lower relisting", integerPointer(600), nil, &ListingAlternative{ListingID: 1, PriceEUR: integerPointer(700)}, true, NotificationRelistingChanged, true},
		{"higher relisting", integerPointer(800), nil, &ListingAlternative{ListingID: 1, PriceEUR: integerPointer(700)}, true, NotificationRelistingChanged, true},
		{"same-price relisting suppressed", integerPointer(700), nil, &ListingAlternative{ListingID: 1, PriceEUR: integerPointer(700)}, true, "", false},
		{"first in-range concurrent offer", integerPointer(600), []ListingAlternative{{ListingID: 1, PriceEUR: integerPointer(900)}}, nil, false, NotificationListingDiscovered, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := ClassifyPropertyDiscovery(test.current, test.active, test.inactive, test.notified)
			if decision.Notify != test.wantAlert || decision.Type != test.wantType {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func integerPointer(value int) *int {
	return &value
}
