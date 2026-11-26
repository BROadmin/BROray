package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

const (
	profileApplyTimeField             = "applyProfileAtUTC"
	profileApplyScheduleRevisionField = "applyProfileScheduleRevision"
	profileApplyMinuteFormat          = "15:04"
	profileApplyDayFormat             = "2006-01-02"
)

var errPendingProfileWindow = errors.New("SUBSCRIPTION_PROFILE_WINDOW_EXPIRED")

type pendingProfileScheduleClaim struct {
	revision string
	day      string
}

func validProfileApplyTime(value any) bool {
	at, ok := value.(string)
	if !ok || at == "" {
		return ok
	}
	parsed, err := time.Parse(profileApplyMinuteFormat, at)
	return err == nil && parsed.Format(profileApplyMinuteFormat) == at
}

func currentProfileSchedule(rec map[string]any, t time.Time) (pendingProfileScheduleClaim, bool) {
	at, _ := rec[profileApplyTimeField].(string)
	token, _ := rec[profileApplyScheduleRevisionField].(string)
	if rec["enabled"] != true || at != t.Format(profileApplyMinuteFormat) || !exactApplyHex(token, 32) {
		return pendingProfileScheduleClaim{}, false
	}
	return pendingProfileScheduleClaim{revision: token, day: t.Format(profileApplyDayFormat)}, true
}

func (s *svc) profileApplyAttempt(subID string, day string) (deletionImage, error) {
	// Settings import and undo swap the whole subscriptions directory. The
	// scheduler's day receipt belongs to the live router across those swaps.
	path := s.d.Path("config", ".profile-apply-attempt-"+compat.SHA256Hex([]byte(subID))+".json")
	previous, err := readDeletionImageLimit(path, true, 256)
	if err != nil || previous.info == nil {
		return previous, err
	}
	if previous.info.Mode().Perm() != 0600 {
		return previous, errors.New("invalid subscription apply attempt permissions")
	}
	value, err := strictjson.Decode(previous.raw)
	if err != nil {
		return previous, err
	}
	record, _ := value.(map[string]any)
	last, _ := record["dayUTC"].(string)
	parsed, err := time.Parse(profileApplyDayFormat, last)
	if len(record) != 2 || !isInt(record["schemaVersion"], 1, 1) || err != nil || parsed.Format(profileApplyDayFormat) != last {
		return previous, errors.New("invalid subscription apply attempt receipt")
	}
	if last >= day {
		return previous, errPendingProfileWindow
	}
	return previous, nil
}

func (s *svc) enqueueDueProfileApply() *compat.Request {
	if s.d.Queue == nil || s.d.Queue.Paused() || s.d.Lock.Busy() {
		return nil
	}
	t := now().UTC()
	for _, id := range s.recordIDs() {
		rec, _, err := s.readRecord(id)
		if err != nil {
			continue
		}
		claim, due := currentProfileSchedule(rec, t)
		if !due {
			continue
		}
		if _, exists := s.d.Queue.FindLive(actionPendingProfileApply, id); exists {
			continue
		}
		pending := s.pendingActiveProfileStatus(id)
		if pending == nil {
			continue
		}
		obs, err := s.observePendingApply(id, pending["revision"])
		if err != nil {
			continue
		}
		if _, err := s.profileApplyAttempt(id, claim.day); err != nil {
			continue
		}
		context := obs.context + ":" + claim.revision + ":" + claim.day
		request, err := s.d.Queue.Enqueue(actionPendingProfileApply, id, "SCHEDULER", context)
		if err == nil {
			return request
		}
		s.d.Log.Printf("subscriptions: enqueue scheduled profile: %v", err)
	}
	return nil
}

func (s *svc) pendingProfileWindow(ctx context.Context, subID string, obs *pendingApplyObservation) (deletionImage, error) {
	if ctx.Err() != nil || !compat.FenceHeld(ctx) || s.d.Queue.Paused() || !obs.binding.unchanged() {
		return deletionImage{}, errPendingProfileWindow
	}
	rec, ok := decodeObject(obs.binding.source.raw)
	if !ok {
		return deletionImage{}, errPendingProfileWindow
	}
	claim, due := currentProfileSchedule(rec, now().UTC())
	if !due || claim != *obs.schedule {
		return deletionImage{}, errPendingProfileWindow
	}
	return s.profileApplyAttempt(subID, claim.day)
}

func (s *svc) claimPendingProfileWindow(ctx context.Context, subID string, obs *pendingApplyObservation) error {
	prior, err := s.pendingProfileWindow(ctx, subID, obs)
	if err != nil {
		return err
	}
	rec, _ := decodeObject(obs.binding.source.raw)
	data, err := json.Marshal(map[string]any{"schemaVersion": 1, "dayUTC": obs.schedule.day})
	if err != nil {
		return err
	}
	if !prior.unchanged() || !obs.binding.source.unchanged() || ctx.Err() != nil || s.d.Queue.Paused() {
		return errPendingProfileWindow
	}
	if claim, due := currentProfileSchedule(rec, now().UTC()); !due || claim != *obs.schedule {
		return errPendingProfileWindow
	}
	if err := platform.WriteFileAtomicNoFollow(prior.path, data, 0600); err != nil {
		return err
	}
	written, err := readDeletionImageLimit(prior.path, false, 256)
	if err != nil || !bytes.Equal(written.raw, data) || written.info.Mode().Perm() != 0600 {
		return errors.New("could not confirm subscription apply attempt receipt")
	}
	return nil
}
