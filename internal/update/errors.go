package update

import (
	"errors"
	"fmt"
)

// Error is a refusal with a stable code (3.x updater codes where one
// existed) and the Russian text the WebUI shows.
type Error struct {
	Code    string
	Message string
	Err     error // diagnostic cause (English), may be nil
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + " (" + e.Err.Error() + ")"
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func fail(code string, cause error, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...), Err: cause}
}

// Code returns the code of an *Error anywhere in the chain ("" otherwise).
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Message returns the Russian message of an *Error, or err.Error().
func Message(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// Codes. HTTPStatus maps them like the 3.x updater API (updater-api-common.sh).
const (
	ErrChannelUnavailable = "CHANNEL_UNAVAILABLE"
	ErrChannelInvalid     = "CHANNEL_INVALID"
	ErrSignatureMissing   = "UPDATE_SIGNATURE_MISSING"
	ErrSignatureInvalid   = "UPDATE_SIGNATURE_INVALID"
	ErrKeyUnknown         = "UPDATE_KEY_UNKNOWN"
	ErrCheckFailed        = "UPDATE_CHECK_FAILED"
	ErrIndexInvalid       = "UPDATE_INDEX_INVALID"
	ErrChannelMismatch    = "UPDATE_CHANNEL_MISMATCH"
	ErrManifestExpired    = "UPDATE_MANIFEST_EXPIRED"
	ErrManifestFuture     = "UPDATE_MANIFEST_FROM_FUTURE"
	ErrClockUnset         = "UPDATE_CLOCK_UNSET"
	ErrNotAvailable       = "UPDATE_NOT_AVAILABLE"
	ErrVersionRevoked     = "UPDATE_VERSION_REVOKED"
	ErrVersionNotListed   = "REINSTALL_SOURCE_UNAVAILABLE"
	ErrArchUnsupported    = "UPDATE_ARCH_UNSUPPORTED"
	ErrSourceTooOld       = "UPDATE_SOURCE_TOO_OLD"
	ErrDownloadFailed     = "UPDATE_DOWNLOAD_FAILED"
	ErrDownloadSize       = "UPDATE_DOWNLOAD_SIZE_MISMATCH"
	ErrDownloadHash       = "UPDATE_DOWNLOAD_HASH_MISMATCH"
	ErrArtifactInvalid    = "UPDATE_ARTIFACT_INVALID"
	ErrSpaceRequired      = "UPDATE_SPACE_REQUIRED"
	ErrBusy               = "OPERATION_BUSY"
	ErrInstallerFailed    = "UPDATE_INSTALLER_FAILED"
)

func HTTPStatus(code string) int {
	switch code {
	case ErrCheckFailed, ErrIndexInvalid, ErrDownloadFailed:
		return 502
	case ErrBusy, ErrNotAvailable:
		return 409
	}
	return 503
}

// Errorf builds an *Error (for the installer, which shares the codes).
func Errorf(code string, cause error, format string, a ...any) *Error {
	return fail(code, cause, format, a...)
}
