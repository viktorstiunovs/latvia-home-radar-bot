package domimaps

import (
	"strings"
	"testing"
)

func TestDecodeJSONPRequiresExactCallbackAndSingleJSONValue(t *testing.T) {
	var payload struct {
		Value int `json:"value"`
	}
	if err := decodeJSONP([]byte(` UMap.ABCDEFGH&&UMap.ABCDEFGH({"value":7}); `), "ABCDEFGH", &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Value != 7 {
		t.Fatalf("value = %d", payload.Value)
	}

	tests := []struct {
		name string
		body string
	}{
		{name: "wrong callback", body: `UMap.XXXXXXXX&&UMap.XXXXXXXX({"value":7});`},
		{name: "plain JSON", body: `{"value":7}`},
		{name: "trailing JavaScript", body: `UMap.ABCDEFGH&&UMap.ABCDEFGH({"value":7});alert(1);`},
		{name: "second JSON value", body: `UMap.ABCDEFGH&&UMap.ABCDEFGH({"value":7}{"value":8});`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := decodeJSONP([]byte(test.body), "ABCDEFGH", &payload); err == nil {
				t.Fatal("invalid JSONP was accepted")
			}
		})
	}
}

func TestNextCallbackMatchesDOMImapsFormat(t *testing.T) {
	callback := nextCallback()
	if len(callback) != 8 || !strings.HasPrefix(callback, "LHR") {
		t.Fatalf("callback = %q", callback)
	}
	for _, char := range callback {
		if !(char >= '0' && char <= '9') && !(char >= 'A' && char <= 'Z') {
			t.Fatalf("callback contains invalid character %q", char)
		}
	}
}
