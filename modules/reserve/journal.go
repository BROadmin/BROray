package reserve

import (
	"errors"
	"os"
	"sync"

	"github.com/BROadmin/broray/internal/platform"
)

const TransactionJournalFile = "server-reserve-pending.v1.json"

var ErrTransactionUnconfirmed = errors.New("RESERVE_TRANSACTION_UNCONFIRMED")

type TransactionImage struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}
type TransactionState struct {
	Config      TransactionImage `json:"config"`
	Preferences TransactionImage `json:"preferences"`
	Active      TransactionImage `json:"active"`
}
type TransactionIntent struct {
	Action                  string `json:"action"`
	Authority               string `json:"authority"`
	InterfaceName           string `json:"interfaceName"`
	Upstream                string `json:"upstream"`
	PreviousDescription     string `json:"previousDescription"`
	PreviousDescriptionLine string `json:"previousDescriptionLine"`
	NextDescription         string `json:"nextDescription"`
	ReceiptSHA256           string `json:"receiptSHA256"`
}
type TransactionRecord struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Phase         string            `json:"phase"`
	Intent        TransactionIntent `json:"intent"`
	Receipt       TransactionImage  `json:"receipt"`
	Previous      TransactionState  `json:"previous"`
	Next          TransactionState  `json:"next"`
}
type TransactionFiles struct{ Config, Preferences, Active []byte }

// The caller must retain the application mutation fence as well as this
// journal. Lifecycle/router proof belongs to the caller's guarded callbacks.
type TransactionJournal struct {
	root          platform.Root
	dirs          map[string]*os.File
	mu            sync.Mutex
	recoveryCheck func() error
}
