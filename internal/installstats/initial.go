package installstats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/update"
)

const firstInstallFile = "first-install.json"

type firstInstall struct {
	EventID string `json:"eventId"`
	Version string `json:"version"`
	SHA256  string `json:"executableSHA256"`
	Created int64  `json:"createdAt"`
	Event   *Event `json:"event,omitempty"`
}

// ArmFirstInstall is called after the first installer successfully starts its
// daemon. A separate proof from the executing daemon gates creation of an event.
func (q *Queue) ArmFirstInstall(version, executableSHA string, now time.Time) error {
	if !update.ValidVersion(version) || !deviceKeyPattern.MatchString(executableSHA) {
		return ErrInvalidEvent
	}
	if err := platform.MkdirAllDurable(q.dir, 0700); err != nil {
		return err
	}
	lock, err := q.locked(".initial.lock")
	if err != nil {
		return err
	}
	defer lock.Release()
	stored, err := q.readFirstInstall()
	if err == nil {
		if stored.Version != version || stored.SHA256 != executableSHA {
			return ErrEventConflict
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	id, err := newEventID()
	if err != nil {
		return err
	}
	intent := firstInstall{EventID: id, Version: version, SHA256: executableSHA}
	if now.Year() >= 2025 && now.Unix() <= lastTimestamp {
		intent.Created = now.Unix()
	}
	return q.writeFirstInstall(intent)
}

// ConfirmFirstInstall freezes the exact observed event before enqueue. Once
// frozen, recovery can resume enqueue without changing the ID or wire body.
func (q *Queue) ConfirmFirstInstall(ctx context.Context, current Build, arch string, now time.Time, prove func(context.Context, string) (Identity, bool)) error {
	// The installer creates the directory before arming the intent. A daemon
	// polling during that interval must leave the installer's lock available.
	if _, err := q.readFirstInstall(); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := q.locked(".initial.lock")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	intent, err := q.readFirstInstall()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	path := filepath.Join(q.dir, firstInstallFile)
	if now.Year() >= 2025 && intent.Created > 0 && now.Unix() >= intent.Created+int64(DeliveryWindow/time.Second) {
		return platform.RemoveRegularNoFollow(path)
	}
	if intent.Event == nil {
		if !current.valid() || current.Version != intent.Version || prove == nil {
			return nil
		}
		identity, ready := prove(ctx, intent.SHA256)
		if !ready || ctx.Err() != nil {
			return nil
		}
		event, err := makeEvent(intent.EventID, "install", current, Build{}, arch, identity)
		if err != nil {
			return err
		}
		intent.Event = &event
		if err := q.writeFirstInstall(intent); err != nil {
			return err
		}
	}
	if err := q.Add(*intent.Event, time.Unix(intent.Created, 0)); err != nil {
		return err
	}
	return platform.RemoveRegularNoFollow(path)
}

func (q *Queue) writeFirstInstall(intent firstInstall) error {
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomicNoFollow(filepath.Join(q.dir, firstInstallFile), data, 0600)
}

func (q *Queue) readFirstInstall() (firstInstall, error) {
	var intent firstInstall
	data, err := q.readBytes(firstInstallFile)
	if err != nil {
		return intent, err
	}
	value, err := strictjson.Decode(data)
	fields, _ := value.(map[string]any)
	if err != nil || (len(fields) != 4 && len(fields) != 5) || fields["eventId"] == nil ||
		fields["version"] == nil || fields["executableSHA256"] == nil || fields["createdAt"] == nil ||
		json.Unmarshal(data, &intent) != nil || !eventIDPattern.MatchString(intent.EventID) ||
		!update.ValidVersion(intent.Version) || !deviceKeyPattern.MatchString(intent.SHA256) ||
		intent.Created < 0 || intent.Created > lastTimestamp {
		return firstInstall{}, errQueueRecord
	}
	if len(fields) == 5 {
		if intent.Event == nil {
			return firstInstall{}, errQueueRecord
		}
		raw, err := json.Marshal(fields["event"])
		if err != nil {
			return firstInstall{}, errQueueRecord
		}
		event, err := ParseEvent(raw)
		if err != nil || event != *intent.Event || event.Type != "install" ||
			event.EventID != intent.EventID || event.Version != intent.Version {
			return firstInstall{}, errQueueRecord
		}
	}
	return intent, nil
}

func ExecutableDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > update.MaxUnpackedBytes {
		return "", ErrInvalidEvent
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, update.MaxUnpackedBytes+1))
	if err != nil || n != info.Size() {
		return "", ErrInvalidEvent
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
