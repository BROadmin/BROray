package installstats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"golang.org/x/sys/unix"
)

const (
	queueCapacity  = 128
	DeliveryWindow = 7 * 24 * time.Hour
	maxAttempts    = 32
	batchSize      = 16
	lastTimestamp  = 253402300799 // 9999-12-31T23:59:59Z
)

var (
	ErrEventConflict = errors.New("installation event ID already identifies different contents")
	ErrQueueFull     = errors.New("installation statistics queue is full")
	errQueueRecord   = errors.New("invalid installation statistics queue record")
)

type Queue struct{ dir string }

type queuedEvent struct {
	Event    Event `json:"event"`
	Created  int64 `json:"createdAt"`
	Next     int64 `json:"nextAttemptAt"`
	Attempts int   `json:"attempts"`
}

type Delivery struct {
	Acknowledged int
	Deferred     int
	Expired      int
}

func NewQueue(root platform.Root) *Queue {
	return &Queue{dir: filepath.Join(root.State(), "install-statistics")}
}

func filename(id string) string { return "event-" + id + ".json" }

func entryID(name string) string {
	if !strings.HasPrefix(name, "event-") || !strings.HasSuffix(name, ".json") {
		return ""
	}
	id := name[len("event-") : len(name)-len(".json")]
	if !eventIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func (q *Queue) locked(name string) (*platform.Lock, error) {
	dir, err := platform.OpenDirNoFollow(q.dir)
	if err != nil {
		return nil, err
	}
	info, err := dir.Stat()
	_ = dir.Close()
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm() != 0700 {
		return nil, errQueueRecord
	}
	return platform.TryLockNoFollow(filepath.Join(q.dir, name))
}

func (q *Queue) entries() ([]os.DirEntry, error) {
	dir, err := platform.OpenDirNoFollow(q.dir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

func (q *Queue) Add(event Event, now time.Time) error {
	if !event.valid() {
		return ErrInvalidEvent
	}
	if err := platform.MkdirAllDurable(q.dir, 0700); err != nil {
		return err
	}
	lock, err := q.locked(".queue.lock")
	if err != nil {
		return err
	}
	defer lock.Release()
	previous, err := q.read(event.EventID)
	if err == nil {
		if previous.Event != event {
			return ErrEventConflict
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := q.entries()
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if entryID(entry.Name()) != "" {
			count++
		}
	}
	if count >= queueCapacity {
		return ErrQueueFull
	}
	var created int64
	if now.Year() >= 2025 && now.Unix() <= lastTimestamp {
		created = now.Unix()
	}
	return q.write(queuedEvent{Event: event, Created: created})
}

func (q *Queue) Flush(ctx context.Context, sender *Sender, now time.Time) (Delivery, error) {
	var delivery Delivery
	if sender == nil || now.Year() < 2025 || now.Unix() > lastTimestamp-int64(DeliveryWindow/time.Second) {
		return delivery, nil
	}
	lock, err := q.locked(".delivery.lock")
	if errors.Is(err, os.ErrNotExist) {
		return delivery, nil
	}
	if err != nil {
		return delivery, err
	}
	defer lock.Release()
	for i := 0; i < batchSize; i++ {
		if err := ctx.Err(); err != nil {
			return delivery, err
		}
		entry, expired, err := q.takeDue(now.Unix())
		delivery.Expired += expired
		if err != nil || entry.Event.EventID == "" {
			return delivery, err
		}
		if sender.send(ctx, entry.Event) {
			if err := q.acknowledge(entry); err != nil {
				return delivery, err
			}
			delivery.Acknowledged++
		} else {
			delivery.Deferred++
		}
	}
	return delivery, nil
}

func (q *Queue) takeDue(now int64) (queuedEvent, int, error) {
	lock, err := q.locked(".queue.lock")
	if err != nil {
		return queuedEvent{}, 0, err
	}
	defer lock.Release()
	entries, err := q.entries()
	if err != nil {
		return queuedEvent{}, 0, err
	}
	expired := 0
	for _, item := range entries {
		id := entryID(item.Name())
		if id == "" {
			continue
		}
		entry, err := q.read(id)
		if err != nil {
			continue // Keep opaque entries intact.
		}
		if entry.Created == 0 {
			entry.Created = now // The event was recorded before the clock settled.
		}
		if now >= entry.Created+int64(DeliveryWindow/time.Second) || entry.Attempts >= maxAttempts {
			if err := platform.RemoveRegularNoFollow(filepath.Join(q.dir, filename(id))); err != nil {
				return queuedEvent{}, expired, err
			}
			expired++
			continue
		}
		if now < entry.Created || now < entry.Next {
			continue
		}
		entry.Attempts++
		entry.Next = now + int64(retryDelay(entry.Attempts)/time.Second)
		// A lost response still consumes an attempt and delays resending.
		if err := q.write(entry); err != nil {
			return queuedEvent{}, expired, err
		}
		return entry, expired, nil
	}
	return queuedEvent{}, expired, nil
}

func retryDelay(attempt int) time.Duration {
	if attempt >= 10 {
		return 6 * time.Hour
	}
	return time.Minute << (attempt - 1)
}

func (q *Queue) acknowledge(sent queuedEvent) error {
	lock, err := q.locked(".queue.lock")
	if err != nil {
		return err
	}
	defer lock.Release()
	current, err := q.read(sent.Event.EventID)
	if err != nil {
		return err
	}
	if current != sent {
		return ErrEventConflict
	}
	return platform.RemoveRegularNoFollow(filepath.Join(q.dir, filename(sent.Event.EventID)))
}

func (q *Queue) write(entry queuedEvent) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomicNoFollow(filepath.Join(q.dir, filename(entry.Event.EventID)), data, 0600)
}

func (q *Queue) readBytes(name string) ([]byte, error) {
	dir, err := platform.OpenDirNoFollow(q.dir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errQueueRecord
	}
	data, err := io.ReadAll(io.LimitReader(f, 2049))
	if err != nil || len(data) > 2048 {
		return nil, errQueueRecord
	}
	return data, nil
}

func (q *Queue) read(id string) (queuedEvent, error) {
	var entry queuedEvent
	data, err := q.readBytes(filename(id))
	if err != nil {
		return entry, err
	}
	value, err := strictjson.Decode(data)
	fields, _ := value.(map[string]any)
	if err != nil || len(fields) != 4 || fields["event"] == nil || fields["createdAt"] == nil || fields["nextAttemptAt"] == nil || fields["attempts"] == nil ||
		json.Unmarshal(data, &entry) != nil || entry.Created < 0 || entry.Created > lastTimestamp ||
		entry.Next < 0 || entry.Next > lastTimestamp || entry.Attempts < 0 || entry.Attempts > maxAttempts {
		return queuedEvent{}, errQueueRecord
	}
	raw, err := json.Marshal(fields["event"])
	if err != nil {
		return queuedEvent{}, errQueueRecord
	}
	event, err := ParseEvent(raw)
	if err != nil || event.EventID != id || event != entry.Event {
		return queuedEvent{}, errQueueRecord
	}
	return entry, nil
}
