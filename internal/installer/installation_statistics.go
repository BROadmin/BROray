package installer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installstats"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

const (
	maxDeferredStatistics = 128
	statisticsDrainBatch  = 8
)

type committedStatistic struct {
	Event       installstats.Event `json:"event"`
	CommittedAt string             `json:"committedAt"`
}

func prepareUpdateStatistics(o ApplyOptions, j *Journal, oldPath string) {
	if o.Revision == "" || j.PreviousSHA256 == j.StagedSHA256 {
		return
	}
	arch := update.Platform()
	previous, ok := installstats.ReadBinaryBuild(context.Background(), oldPath, j.From, arch, j.PreviousSHA256)
	if !ok {
		return
	}
	build := installstats.Build{Version: j.To, Revision: o.Revision}
	event, err := installstats.NewEvent("update", build, previous, arch, installstats.Identity{})
	if err == nil {
		j.Statistics = &event
	}
}

func confirmUpdateStatistics(root platform.Root, j *Journal) {
	if j.Statistics == nil || j.State != JStarted {
		return
	}
	live, err := control.ObserveBuild(context.Background(), root, j.StagedSHA256)
	event := j.Statistics
	if err != nil || live.SHA256 != j.StagedSHA256 || live.Version != event.Version ||
		live.Revision != event.Revision || live.Architecture != event.Architecture {
		return
	}
	event.Model = live.Model
	event.DeviceKey = live.DeviceKey
	j.StatisticsReady = true
}

// carryCommittedStatistics supplies the already committed observations to
// write atomically as part of the next operation's prepared journal.
func carryCommittedStatistics(previous *Journal) []committedStatistic {
	if previous == nil || previous.SchemaVersion != 1 {
		return nil
	}
	deferred := previous.DeferredStatistics
	if pendingStatistics(previous) {
		deferred = append(deferred, committedStatistic{Event: *previous.Statistics, CommittedAt: previous.UpdatedAt})
	}
	if len(deferred) > maxDeferredStatistics {
		deferred = deferred[len(deferred)-maxDeferredStatistics:]
	}
	return deferred
}

// The installer lock must be held. The journal owns the frozen observations
// until the outbox accepts them or they exceed the delivery window.
func drainCommittedStatistics(root platform.Root, j *Journal) {
	if !hasCommittedStatistics(j) {
		return
	}
	queue := installstats.NewQueue(root)
	now := time.Now()
	cleared := *j
	cleared.DeferredStatistics = nil
	attempts := 0
	for i, record := range j.DeferredStatistics {
		if attempts == statisticsDrainBatch {
			cleared.DeferredStatistics = append(cleared.DeferredStatistics, j.DeferredStatistics[i:]...)
			break
		}
		attempts++
		err := moveCommittedStatistic(queue, record, now)
		if err == nil {
			continue
		}
		if errors.Is(err, platform.ErrLocked) || errors.Is(err, installstats.ErrQueueFull) {
			cleared.DeferredStatistics = append(cleared.DeferredStatistics, j.DeferredStatistics[i:]...)
			attempts = statisticsDrainBatch
			break
		}
		cleared.DeferredStatistics = append(cleared.DeferredStatistics, record)
	}
	if pendingStatistics(j) && attempts < statisticsDrainBatch &&
		moveCommittedStatistic(queue, committedStatistic{Event: *j.Statistics, CommittedAt: j.UpdatedAt}, now) == nil {
		cleared.Statistics = nil
		cleared.StatisticsReady = false
	}
	if len(cleared.DeferredStatistics) == len(j.DeferredStatistics) && cleared.Statistics == j.Statistics {
		return
	}
	data, err := json.Marshal(&cleared)
	if err == nil && platform.WriteFileAtomicNoFollow(root.UpdateJournal(), data, 0600) == nil {
		*j = cleared
	}
}

func moveCommittedStatistic(queue *installstats.Queue, record committedStatistic, now time.Time) error {
	when, err := time.Parse(time.RFC3339, record.CommittedAt)
	if err != nil {
		return err
	}
	if now.Year() >= update.MinClockYear && when.Year() >= update.MinClockYear && now.Sub(when) >= installstats.DeliveryWindow {
		return nil
	}
	return queue.Add(record.Event, when)
}

func hasCommittedStatistics(j *Journal) bool {
	return j != nil && j.SchemaVersion == 1 && (len(j.DeferredStatistics) > 0 || pendingStatistics(j))
}

func pendingStatistics(j *Journal) bool {
	return j != nil && j.SchemaVersion == 1 && j.State == JCommitted && j.StatisticsReady && j.Statistics != nil &&
		validateCommitJournal(j) == nil && j.Statistics.Type == "update" &&
		j.Statistics.Version == j.To && j.Statistics.PreviousVersion == j.From
}

func RetryCommittedStatistics(root platform.Root) {
	j := readStatisticsJournal(root)
	if !hasCommittedStatistics(j) {
		return
	}
	lock, err := platform.TryLockNoFollow(root.InstallerLock())
	if err != nil {
		return
	}
	defer lock.Release()
	drainCommittedStatistics(root, readStatisticsJournal(root))
}

func readStatisticsJournal(root platform.Root) *Journal {
	_, raw, err := readHandoffFile(root.UpdateJournal())
	if err != nil {
		return nil
	}
	var j Journal
	if json.Unmarshal(raw, &j) != nil {
		return nil
	}
	return &j
}
