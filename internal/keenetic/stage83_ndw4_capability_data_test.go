package keenetic

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The actual KeeneticOS 5.2 Beta 1 on a KN-2710 advertises a canonical
// empty JSON data field in its x-ndw4-interactive challenge. This is a
// capability marker, NOT the subsequent password-proof X-NDM-Data header.
// The test uses purely synthetic session values and zero real credentials.
func TestStage83NativeNDW4EmptyDataCapabilityChoosesPasswordNDW4(t *testing.T) {
	header := http.Header{}
	header.Add("WWW-Authenticate", `x-ndw2-interactive realm="Synthetic Router" challenge="synthetic" session_id="abcd" session_cookie="efgh"`)
	header.Add("WWW-Authenticate", `x-ndw4-interactive endpoint="/auth" data="e30="`)
	mode, err := selectAuthMode(header)
	if err != nil || mode != authNDW4 {
		t.Fatalf("real native 5.2 NDW4 capability rejected before password verification: mode=%d err=%v", mode, err)
	}
}

func TestStage83NativeCapabilityOptionalEmptyDataRemainsStrict(t *testing.T) {
	for _, raw := range []string{
		`x-ndw4-interactive`,
		`x-ndw4-interactive endpoint="/auth"`,
		`x-ndw4-interactive endpoint="/auth" data="e30="`,
		`X-NDW4-Interactive endpoint="/auth" data="e30="`,
	} {
		h := http.Header{}
		h.Add("WWW-Authenticate", raw)
		mode, err := selectAuthMode(h)
		if err != nil || mode != authNDW4 {
			t.Fatalf("supported NDW4 capability blocked: %q mode=%d err=%v", raw, mode, err)
		}
	}
}

func TestStage83NDW4NativeCapabilityRejectsAlteredDataAndUnknownFeatures(t *testing.T) {
	for _, raw := range []string{
		`x-ndw4-interactive endpoint="/auth" data="e31="`,         // invalid base64
		`x-ndw4-interactive endpoint="/auth" data="e30"`,          // padding omitted
		`x-ndw4-interactive endpoint="/auth" data="W10="`,         // [] not a JSON object
		`x-ndw4-interactive endpoint="/auth" data="eyJ4IjoxfQ=="`, // {"x":1}, unknown future policy
		`x-ndw4-interactive endpoint="/auth" data=""`,
		`x-ndw4-interactive endpoint="/auth" data=e30=`,
		`x-ndw4-interactive data="e30=" endpoint="/auth"`,
		`x-ndw4-interactive data="e30="`,
		`x-ndw4-interactive endpoint="/auth" data="e30=" data="e30="`,
		`x-ndw4-interactive endpoint="/auth" data="e30=" unknown="1"`,
		`x-ndw4-interactive endpoint="/evil" data="e30="`,
		`x-ndw4-interactive endpoint="http://evil.test" data="e30="`,
		`x-ndw4-interactive endpoint="/auth" data="e30="\r\nx-new-header: injected`,
		`x-ndw4-interactive endpoint="/auth" data="e30="; upgrade=true`,
		`x-ndw4-interactive endpoint="/auth" data="e30=",x-other-auth`,
		`x-ndw4-interactive endpoint="/auth" data="e30=" x-ndw2-interactive`,
		`x-ndw4-authenticate`, // access-token bootstrap is not a password protocol
	} {
		h := http.Header{}
		h.Add("WWW-Authenticate", raw)
		_, err := selectAuthMode(h)
		if !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrProtocol) {
			t.Fatalf("dangerous or future capability accepted: %q err=%v", raw, err)
		}
	}
}

func TestStage83NoNDW4ToLegacyDowngradeOnUnknownCapability(t *testing.T) {
	for _, strange := range []string{
		`x-ndw4-interactive endpoint="/auth" data="eyJ4IjoxfQ=="`,
		`x-ndw4-authenticate`,
	} {
		h := http.Header{}
		h.Add("WWW-Authenticate", `x-ndw2-interactive`)
		h.Add("WWW-Authenticate", strange)
		_, err := selectAuthMode(h)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("legacy downgrade accepted with unrecognized NDW4 capability %q: %v", strange, err)
		}
	}
}

func TestStage83RejectExcessivelyLargeCapabilityParameters(t *testing.T) {
	h := http.Header{}
	h.Add("WWW-Authenticate", `x-ndw4-interactive endpoint="/auth" data="`+strings.Repeat("A", 8193)+`"`)
	_, err := selectAuthMode(h)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unbounded capability data accepted: %v", err)
	}
}
