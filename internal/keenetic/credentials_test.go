package keenetic

import (
	"context"
	"testing"
)

func TestBROrayLoginTrimContract(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{" \tSyntheticUser\r\n", "SyntheticUser"},
		{"\ufeff\u00a0Пользователь\u3000", "Пользователь"},
		{"\u2000\u200auser\u2028\u2029", "user"},
		{"\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff", ""},
		{"User Name", "User Name"},
		{"\u0085user\u0085", "\u0085user\u0085"},
		{"\u200buser\u200b", "\u200buser\u200b"},
		{" E\u0301USER ", "E\u0301USER"},
	} {
		if NormalizeLogin(c.input) != c.want || NormalizeLogin(NormalizeLogin(c.input)) != c.want {
			t.Fatal("frontend login normalization mismatch")
		}
	}
}

func TestBROrayTrimAppliesBeforeVerify(t *testing.T) {
	f := newFake()
	f.password = " \tSyntheticPassword\n\ufeff"
	v := setup(t, f)
	if err := v.Verify(context.Background(), " \ufeff"+f.login+"\t", f.password); err != nil {
		t.Fatal("trimmed login with unchanged password rejected")
	}
	if f.gets != 1 || f.posts != 1 || !f.cookieOK {
		t.Fatal("cookie continuity or single-send contract changed")
	}
}

func TestBROrayWhitespaceOnlyLoginDoesNotContactRouter(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	if v.Verify(context.Background(), " \ufeff\t", f.password) != ErrInvalidCredentials || f.gets != 0 || f.posts != 0 {
		t.Fatal("empty normalized login contacted router")
	}
}
