// Package installstats owns the wire contract and local delivery of confirmed
// installation events. Runtime readiness is proved by the installer.
package installstats

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/update"
)

var ErrInvalidEvent = errors.New("invalid installation event")

var (
	eventIDPattern   = regexp.MustCompile(`^[a-f0-9]{32}$`)
	deviceKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	serialPattern    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{5,47}$`)
	revisionPattern  = regexp.MustCompile(`^[A-Za-z0-9]{1,12}(\.[A-Za-z0-9]{1,12}){0,2}$`)
)

type Build struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

func (b Build) valid() bool {
	return update.ValidVersion(b.Version) && revisionPattern.MatchString(b.Revision)
}

type Identity struct {
	Model     string
	DeviceKey string
}

// RouterIdentity transforms the locally read serial before it can enter the
// event or outbox. A device key is for counting, never sender authentication.
func RouterIdentity(model, serial string) Identity {
	var identity Identity
	model = strings.ToUpper(strings.TrimSpace(model))
	if keenetic.IsRouterModelCode(model) {
		identity.Model = model
	} else {
		identity.Model = keenetic.RouterModelSuffix(model)
	}
	serial = strings.ToUpper(strings.TrimSpace(serial))
	if serialPattern.MatchString(serial) {
		sum := sha256.Sum256([]byte("broray-router-key-v1\x00" + serial))
		identity.DeviceKey = hex.EncodeToString(sum[:])
	}
	return identity
}

type Event struct {
	EventID          string `json:"eventId"`
	Type             string `json:"type"`
	Version          string `json:"version"`
	Revision         string `json:"revision"`
	Architecture     string `json:"architecture"`
	Model            string `json:"model,omitempty"`
	DeviceKey        string `json:"deviceKey,omitempty"`
	PreviousVersion  string `json:"previousVersion,omitempty"`
	PreviousRevision string `json:"previousRevision,omitempty"`
}

func NewEvent(kind string, installed, previous Build, arch string, identity Identity) (Event, error) {
	id, err := newEventID()
	if err != nil {
		return Event{}, err
	}
	return makeEvent(id, kind, installed, previous, arch, identity)
}

func newEventID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func makeEvent(id, kind string, installed, previous Build, arch string, identity Identity) (Event, error) {
	event := Event{
		EventID: id, Type: kind, Version: installed.Version, Revision: installed.Revision,
		Architecture: arch, Model: identity.Model, DeviceKey: identity.DeviceKey,
		PreviousVersion: previous.Version, PreviousRevision: previous.Revision,
	}
	if !event.valid() {
		return Event{}, ErrInvalidEvent
	}
	return event, nil
}

func (e Event) valid() bool {
	switch e.Architecture {
	case "arm64", "arm/7", "mips/softfloat", "mipsle/softfloat":
	default:
		return false
	}
	current := Build{e.Version, e.Revision}
	previous := Build{e.PreviousVersion, e.PreviousRevision}
	if !eventIDPattern.MatchString(e.EventID) || !current.valid() ||
		e.Model != "" && !keenetic.IsRouterModelCode(e.Model) || e.DeviceKey != "" && !deviceKeyPattern.MatchString(e.DeviceKey) {
		return false
	}
	switch e.Type {
	case "install":
		return previous == (Build{})
	case "update":
		return previous.valid() && previous != current
	}
	return false
}

func ParseEvent(raw []byte) (Event, error) {
	if len(raw) > 1024 {
		return Event{}, ErrInvalidEvent
	}
	value, err := strictjson.Decode(raw)
	fields, ok := value.(map[string]any)
	if err != nil || !ok {
		return Event{}, ErrInvalidEvent
	}
	for name, value := range fields {
		switch name {
		case "eventId", "type", "version", "revision", "architecture", "model", "deviceKey", "previousVersion", "previousRevision":
		default:
			return Event{}, ErrInvalidEvent
		}
		text, ok := value.(string)
		if !ok || text == "" {
			return Event{}, ErrInvalidEvent
		}
	}
	var event Event
	if json.Unmarshal(raw, &event) != nil || !event.valid() {
		return Event{}, ErrInvalidEvent
	}
	return event, nil
}
