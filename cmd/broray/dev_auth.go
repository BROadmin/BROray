//go:build dev

package main

import (
	"context"
	"os"
	"strings"

	"github.com/BROadmin/broray/internal/keenetic"
)

// devVerifier is compiled only with -tags dev (host testing): it accepts the
// login:password pair from BRORAY_DEV_LOGIN instead of asking KeeneticOS.
// Never part of a router build.
func init() {
	if v := os.Getenv("BRORAY_DEV_LOGIN"); v != "" {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) == 2 {
			devVerifierOverride = &devVerifier{login: parts[0], password: parts[1]}
		}
	}
}

type devVerifier struct{ login, password string }

func (d *devVerifier) Verify(_ context.Context, login, password string) error {
	if login == d.login && password == d.password {
		return nil
	}
	return keenetic.ErrInvalidCredentials
}
