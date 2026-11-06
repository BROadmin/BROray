package keenetic

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// This is only a synthetic 128/256 MiB acceptance-envelope model, NOT a
// physical claim about real 128/256 MiB Keenetic hardware.
func TestNDW4ConservativeRouterMemoryProfiles(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		totalMiB, availableMiB, costKiB uint32
		want                            error
	}{
		{"128MiB_low_available_cannot_run_64MiB_argon", 128, 58, 65536, ErrResource},
		{"128MiB_small_kdf_can_run_if_memory_available", 128, 58, 8192, nil},
		{"128MiB_border_refuses_64MiB", 128, 95, 65536, ErrResource},
		{"128MiB_exact_margin_64MiB", 128, 96, 65536, nil},
		{"256MiB_low_ram_refuses_64MiB", 256, 64, 65536, ErrResource},
		{"256MiB_ample_available_64MiB", 256, 164, 65536, nil},
		{"256MiB_running_many_services_refuses_32MiB", 256, 52, 32768, ErrResource},
		{"256MiB_sufficient_32MiB", 256, 104, 32768, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.availableMiB > tc.totalMiB {
				t.Fatal("invalid simulated profile")
			}
			got := checkNDW4Memory(tc.costKiB, func() (uint64, error) {
				return uint64(tc.availableMiB) << 20, nil
			})
			if !errors.Is(got, tc.want) {
				t.Fatalf("synthetic only costKiB=%d MemAvailableMiB=%d result=%v", tc.costKiB, tc.availableMiB, got)
			}
		})
	}
}
func TestNDW4MemoryReadFailureRefusesBeforeKDF(t *testing.T) {
	for _, want := range []func() (uint64, error){
		func() (uint64, error) { return 0, ErrResource },
		func() (uint64, error) { return 0, nil },
		func() (uint64, error) { return 8 << 20, nil },
	} {
		if got := checkNDW4Memory(8192, want); !errors.Is(got, ErrResource) {
			t.Fatalf("mem read failed open: %v", got)
		}
	}
}
func TestNDW4ConcurrentSlotCancelDoesNotWaitForOtherKDF(t *testing.T) {
	v := &Verifier{}
	release, err := v.acquireNDW4(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release() // prevent a leaked held token if test fails
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := v.acquireNDW4(ctx)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("a second NDW4 request entered an occupied KDF guard: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cancel not propagated: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled request trapped in Argon2 guard wait")
	}
}
func TestNDW4ZeroLengthAndCancelledContextRefuseWithoutSlotUse(t *testing.T) {
	v := &Verifier{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.acquireNDW4(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancelled ctx admitted: %v", err)
	}
	release, err := v.acquireNDW4(context.Background())
	if err != nil {
		t.Fatalf("admission broken after cancel: %v", err)
	}
	release()
	release, err = v.acquireNDW4(context.Background())
	if err != nil {
		t.Fatalf("guard not released: %v", err)
	}
	release()
}
func TestNDW4CommonBoundaryRejectsOverlongSecretBeforeRouter(t *testing.T) {
	cases := []struct {
		name, login, password string
	}{
		{"login_129_runes", strings.Repeat("L", 129), "test"},
		{"password_513_runes", "test", strings.Repeat("P", 513)},
		{"login_invalid_utf8", string([]byte{255}), "test"},
		{"password_invalid_utf8", "test", string([]byte{255})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			v := setup(t, f)
			if got := v.Verify(context.Background(), tc.login, tc.password); !errors.Is(got, ErrInvalidCredentials) {
				t.Fatalf("invalid input admitted: %v", got)
			}
			if f.gets != 0 || f.posts != 0 {
				t.Fatal("unsafe credential reached management router")
			}
		})
	}
}
func TestNDW4CapabilityAmbiguitiesRefuseNoPasswordDowngrade(t *testing.T) {
	cases := []struct {
		name   string
		fields []string
		want   error
	}{
		{"two_real_fields", []string{"x-ndw2-interactive", `x-ndw4-interactive endpoint="/auth"`}, nil},
		{"duplicate_mode4", []string{"x-ndw4-interactive", "x-ndw4-interactive"}, ErrProtocol},
		{"unapproved_target", []string{`x-ndw4-interactive endpoint="/unapproved"`}, ErrUnsupported},
		{"bootstrap_marker_is_not_password_protocol", []string{"x-ndw2-interactive", "x-ndw4-authenticate"}, ErrUnsupported},
		{"unknown_mode_plus_ndw4", []string{"x-ndw4-interactive", "arbitrary-future-auth"}, ErrUnsupported},
		{"ndw4_excess_parameter", []string{`x-ndw4-interactive endpoint="/auth" evil="1"`}, ErrUnsupported},
		{"ndw4_unquoted_endpoint", []string{"x-ndw4-interactive endpoint=/auth"}, ErrUnsupported},
		{"whitespace_suspicious", []string{"x-ndw4-interactive\nx-ndw2-interactive"}, ErrUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{"Www-Authenticate": tc.fields}
			mode, err := selectAuthMode(h)
			if tc.want == nil {
				if err != nil || mode != authNDW4 {
					t.Fatalf("physical dual capability rejected: mode=%d err=%v", mode, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("unsupported or ambiguous capability accepted: %v", err)
			}
		})
	}
}
