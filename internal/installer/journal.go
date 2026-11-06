package installer

import (
	"errors"
	"os"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// Journal is the durable record of one update/reinstall
// (state/update-journal.json, written with fsync + rename + dir fsync).
// States: prepared → stopping → switching → started → committed, or
// rolling-back → rolled-back after a failure past the switch, or failed
// when the operation stopped before anything was switched. After a power
// loss `broray ctl recover` (run by S24broray before start) reads it and
// leaves exactly one version current.
type Journal struct {
	SchemaVersion    int            `json:"schemaVersion"`
	OperationID      string         `json:"operationId"`
	Operation        string         `json:"operation"` // update | reinstall
	Mode             string         `json:"mode"`      // switch (current → versions/<to>) | replace (same version, binary swap)
	From             string         `json:"from"`
	To               string         `json:"to"`
	Channel          string         `json:"channel"`
	PreviousTarget   string         `json:"previousTarget"` // readlink(current) before the operation
	NewTarget        string         `json:"newTarget"`
	CreatedDir       bool           `json:"createdVersionDir"`
	PreviousSHA256   string         `json:"previousSha256,omitempty"`
	StagedSHA256     string         `json:"stagedSha256,omitempty"`
	OwnedVersions    []OwnedVersion `json:"ownedVersions,omitempty"`
	InitBackup       string         `json:"initBackup,omitempty"`
	DaemonWasRunning bool           `json:"daemonWasRunning"`
	State            string         `json:"state"`
	Error            string         `json:"error,omitempty"`
	History          []JournalStep  `json:"history"`
	UpdatedAt        string         `json:"updatedAt"`
}

// OwnedVersion records a version that was current during an installer
// operation. The exact binary digest is checked again before any deletion.
// Carrying this proof in each journal preserves it across reinstalls and
// interrupted updates, which replace the previous journal file.
type OwnedVersion struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type JournalStep struct {
	State string `json:"state"`
	At    string `json:"at"`
}

const (
	JPrepared    = "prepared"
	JStopping    = "stopping"
	JSwitching   = "switching"
	JStarted     = "started"
	JCommitted   = "committed"
	JRollingBack = "rolling-back"
	JRolledBack  = "rolled-back"
	JFailed      = "failed"
)

// Terminal reports a finished operation (nothing for recover to do).
func (j *Journal) Terminal() bool {
	return j.State == JCommitted || j.State == JRolledBack || j.State == JFailed
}

// LoadJournal returns nil, nil when no journal exists.
func LoadJournal(root platform.Root) (*Journal, error) {
	var j Journal
	err := platform.ReadJSON(root.UpdateJournal(), &j)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// CrashAfter, when set (host-test build only, -tags brorayfake), makes the
// process exit right after the journal reaches that state — the fault
// injection of design §9. Router builds never set it.
var CrashAfter string

func (j *Journal) set(root platform.Root, state string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	j.SchemaVersion = 1
	j.State = state
	j.UpdatedAt = now
	j.History = append(j.History, JournalStep{State: state, At: now})
	if err := os.MkdirAll(root.State(), 0o700); err != nil {
		return err
	}
	if err := platform.WriteJSONAtomic(root.UpdateJournal(), j, 0o600); err != nil {
		return err
	}
	if CrashAfter != "" && CrashAfter == state {
		os.Exit(97)
	}
	return nil
}
