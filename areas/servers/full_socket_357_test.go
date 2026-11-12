package servers

import (
	"encoding/json"
	"testing"
)

func TestFullSocket357TransportNumericBounds(t *testing.T) {
	for _, tc := range []struct {
		network, block, key string
		value               any
	}{
		{"ws", "wsSettings", "heartbeatPeriod", json.Number("4294967295")},
		{"grpc", "grpcSettings", "idle_timeout", json.Number("-1")},
		{"grpc", "grpcSettings", "health_check_timeout", json.Number("-2147483648")},
		{"grpc", "grpcSettings", "initial_windows_size", json.Number("-1")},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if e := ValidateStreamOptions(tc.network, map[string]any{tc.block: map[string]any{tc.key: tc.value}}); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, tc := range []struct {
		network, block, key string
		value               any
	}{
		{"ws", "wsSettings", "heartbeatPeriod", json.Number("4294967296")},
		{"ws", "wsSettings", "heartbeatPeriod", json.Number("-1")},
		{"grpc", "grpcSettings", "idle_timeout", json.Number("2147483648")},
		{"grpc", "grpcSettings", "initial_windows_size", json.Number("-2147483649")},
	} {
		if e := ValidateStreamOptions(tc.network, map[string]any{tc.block: map[string]any{tc.key: tc.value}}); e == nil {
			t.Fatal("overflow accepted")
		}
	}
}
