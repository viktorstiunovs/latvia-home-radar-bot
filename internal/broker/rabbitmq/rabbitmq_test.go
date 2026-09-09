package rabbitmq

import "testing"

func TestRetryCountAcceptsAMQPIntegerTypes(t *testing.T) {
	tests := []struct {
		value any
		want  int64
	}{
		{value: int8(1), want: 1},
		{value: int16(2), want: 2},
		{value: int32(3), want: 3},
		{value: int64(4), want: 4},
		{value: int(5), want: 5},
		{value: "invalid", want: 0},
	}
	for _, test := range tests {
		if actual := retryCount(test.value); actual != test.want {
			t.Fatalf("retryCount(%v) = %d, want %d", test.value, actual, test.want)
		}
	}
}
