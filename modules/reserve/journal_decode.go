package reserve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/strictjson"
)

const SourceProfileTransactionAction = "source-profile-apply"
const SourceEmergencyProfileTransactionAction = "source-emergency-profile-apply"

const maxTransactionConfigBytes = 2 << 20
const maxTransactionReceiptBytes = 64 << 10
const maxTransactionJournalBytes = 6 << 20

var transactionHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var transactionInterfacePattern = regexp.MustCompile(`^Proxy[0-9]{1,5}$`)
var transactionServerID = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,127}$`)

// Every field is mandatory, including explicit existence flags. An omitted
// predecessor must never become permission to delete a file during rollback.
func transactionFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrTransactionUnconfirmed
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok || !allowed[name] || fields[name] != nil {
			return nil, ErrTransactionUnconfirmed
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(fields) != len(names) {
		return nil, ErrTransactionUnconfirmed
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrTransactionUnconfirmed
	}
	return fields, nil
}
func transactionImageFields(raw []byte) error {
	f, err := transactionFields(raw, "exists", "data", "sha256")
	if err != nil {
		return err
	}
	flag := string(bytes.TrimSpace(f["exists"]))
	if flag != "true" && flag != "false" {
		return ErrTransactionUnconfirmed
	}
	var digest string
	if bytes.Equal(bytes.TrimSpace(f["sha256"]), []byte("null")) || json.Unmarshal(f["sha256"], &digest) != nil {
		return ErrTransactionUnconfirmed
	}
	data := bytes.TrimSpace(f["data"])
	if flag == "false" {
		if !bytes.Equal(data, []byte("null")) {
			return ErrTransactionUnconfirmed
		}
	} else {
		var value string
		if len(data) == 0 || data[0] != '"' || json.Unmarshal(data, &value) != nil {
			return ErrTransactionUnconfirmed
		}
	}
	return nil
}
func decodeTransactionRecord(raw []byte) (TransactionRecord, error) {
	var r TransactionRecord
	if len(raw) > maxTransactionJournalBytes || !utf8.Valid(raw) {
		return r, ErrTransactionUnconfirmed
	}
	f, err := transactionFields(raw, "schemaVersion", "id", "phase", "intent", "receipt", "previous", "next")
	if err != nil {
		return r, err
	}
	intent, err := transactionFields(f["intent"], "action", "authority", "interfaceName", "upstream", "previousDescription", "previousDescriptionLine", "nextDescription", "receiptSHA256")
	if err != nil {
		return r, err
	}
	for _, value := range intent {
		var s string
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &s) != nil {
			return r, ErrTransactionUnconfirmed
		}
	}
	if err := transactionImageFields(f["receipt"]); err != nil {
		return r, err
	}
	for _, name := range []string{"previous", "next"} {
		state, err := transactionFields(f[name], "config", "preferences", "active")
		if err != nil {
			return r, err
		}
		for _, image := range state {
			if err := transactionImageFields(image); err != nil {
				return r, err
			}
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	return r, r.validate()
}
func transactionObject(data []byte) bool {
	v, err := strictjson.Decode(data)
	_, ok := v.(map[string]any)
	return err == nil && ok
}
func transactionDescription(s string) bool {
	if s == "" || len(s) > 512 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (r TransactionRecord) validate() error {
	if r.SchemaVersion != 1 || !transactionHashPattern.MatchString(r.ID) || (r.Phase != "prepared" && r.Phase != "committed") {
		return ErrTransactionUnconfirmed
	}
	i := r.Intent
	if !strings.HasPrefix(i.PreviousDescriptionLine, "    description ") || len(i.PreviousDescriptionLine) > 2048 || !utf8.ValidString(i.PreviousDescriptionLine) {
		return ErrTransactionUnconfirmed
	}
	for _, c := range i.PreviousDescriptionLine {
		if unicode.IsControl(c) {
			return ErrTransactionUnconfirmed
		}
	}
	if !transactionHashPattern.MatchString(i.Authority) || !transactionHashPattern.MatchString(i.ReceiptSHA256) || !transactionInterfacePattern.MatchString(i.InterfaceName) || !transactionDescription(i.PreviousDescription) || !transactionDescription(i.NextDescription) {
		return ErrTransactionUnconfirmed
	}
	endpoint, err := netip.ParseAddrPort(i.Upstream)
	if err != nil || endpoint.Port() == 0 || !endpoint.Addr().Is4() || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() {
		return ErrTransactionUnconfirmed
	}
	switch i.Action {
	case "enable":
		if i.NextDescription != "BROray - mix-auto" {
			return ErrTransactionUnconfirmed
		}
	case "disable":
		if i.PreviousDescription != "BROray - mix-auto" {
			return ErrTransactionUnconfirmed
		}
	case "switch":
		if i.PreviousDescription != "BROray - mix-auto" || i.NextDescription != "BROray - mix-auto" {
			return ErrTransactionUnconfirmed
		}
	case "exit-switch":
		previousEnabled, known := transactionReserveEnabled(r.Previous.Preferences)
		if i.PreviousDescription != "BROray - mix-auto" || i.NextDescription == "BROray - mix-auto" ||
			!r.Previous.Config.Exists || !transactionActive(r.Previous.Active) ||
			!known || !previousEnabled || !r.Next.Preferences.Exists || !transactionReserveOff(r.Next.Preferences) {
			return ErrTransactionUnconfirmed
		}
	case SourceProfileTransactionAction:
		if !r.Previous.Config.Exists || !transactionActive(r.Previous.Active) ||
			!r.Previous.Active.equal(r.Next.Active) || !r.Previous.Preferences.equal(r.Next.Preferences) {
			return ErrTransactionUnconfirmed
		}
	case SourceEmergencyProfileTransactionAction:
		if !r.Previous.Config.Exists || !r.Next.Config.Exists || !transactionActive(r.Previous.Active) ||
			!r.Previous.Active.equal(r.Next.Active) || i.PreviousDescription != "BROray - mix-auto" ||
			i.NextDescription != i.PreviousDescription || !transactionEmergencyProfileChange(r.Previous.Preferences, r.Next.Preferences,
			strings.TrimSpace(string(r.Previous.Active.Data))) {
			return ErrTransactionUnconfirmed
		}
		enabled, known := transactionReserveEnabled(r.Previous.Preferences)
		if r.Previous.Preferences.Exists && !known ||
			(i.PreviousDescription == "BROray - mix-auto") != enabled ||
			(i.NextDescription == "BROray - mix-auto") != enabled {
			return ErrTransactionUnconfirmed
		}
	case "source-replace", "source-withdraw":
		// Source removal first disables reserve through its own confirmed
		// transaction. It may not borrow a live roster or mix-auto ownership.
		if i.PreviousDescription == "BROray - mix-auto" || i.NextDescription == "BROray - mix-auto" ||
			!r.Previous.Config.Exists || !transactionActive(r.Previous.Active) ||
			!transactionReserveOff(r.Previous.Preferences) || !transactionReserveOff(r.Next.Preferences) {
			return ErrTransactionUnconfirmed
		}
		if i.Action == "source-withdraw" && (r.Next.Config.Exists || r.Next.Active.Exists || i.NextDescription != i.PreviousDescription) {
			return ErrTransactionUnconfirmed
		}
	default:
		return ErrTransactionUnconfirmed
	}
	if !r.Receipt.Exists || !r.Receipt.valid(maxTransactionReceiptBytes) || r.Receipt.SHA256 != i.ReceiptSHA256 || !transactionObject(r.Receipt.Data) {
		return ErrTransactionUnconfirmed
	}
	for _, s := range []TransactionState{r.Previous, r.Next} {
		if !s.Config.valid(maxTransactionConfigBytes) || !s.Preferences.valid(4096) || !s.Active.valid(4096) {
			return ErrTransactionUnconfirmed
		}
		if s.Config.Exists && !transactionObject(s.Config.Data) {
			return ErrTransactionUnconfirmed
		}
		if s.Preferences.Exists && !transactionObject(s.Preferences.Data) {
			return ErrTransactionUnconfirmed
		}
	}
	if i.Action != "source-withdraw" && (!r.Next.Config.Exists || !transactionActive(r.Next.Active)) {
		return ErrTransactionUnconfirmed
	}
	if i.Action != "source-withdraw" && i.Action != "source-replace" && i.Action != SourceProfileTransactionAction && !r.Next.Preferences.Exists {
		return ErrTransactionUnconfirmed
	}
	return nil
}

func transactionActive(image TransactionImage) bool {
	return image.Exists && utf8.Valid(image.Data) && strings.TrimSpace(string(image.Data)) != "" && !bytes.ContainsRune(image.Data, 0)
}

type transactionEmergencyScope struct {
	primary, fingerprint string
	backups              []string
}

func transactionEmergencyProfileChange(previous, next TransactionImage, active string) bool {
	before, ok := transactionEmergencyPreferences(previous, active)
	after, okNext := transactionEmergencyPreferences(next, active)
	return ok && okNext && before.primary == after.primary && slices.Equal(before.backups, after.backups) &&
		before.fingerprint != after.fingerprint
}

func transactionEmergencyPreferences(image TransactionImage, active string) (transactionEmergencyScope, bool) {
	if !image.Exists || !transactionServerID.MatchString(active) {
		return transactionEmergencyScope{}, false
	}
	v, err := strictjson.Decode(image.Data)
	m, _ := v.(map[string]any)
	emergency, _ := m["emergency"].(map[string]any)
	fp, _ := emergency["fingerprint"].(string)
	primary, _ := m["primaryServerId"].(string)
	raw, _ := m["backupServerIds"].([]any)
	if err != nil || len(m) != 5 || m["schemaVersion"] != json.Number("1") || m["enabled"] != true ||
		len(emergency) != 2 || emergency["serverId"] != active || !transactionHashPattern.MatchString(fp) ||
		!transactionServerID.MatchString(primary) || primary == active || len(raw) == 0 || len(raw) > 2 {
		return transactionEmergencyScope{}, false
	}
	scope := transactionEmergencyScope{primary: primary, fingerprint: fp}
	for _, value := range raw {
		id, _ := value.(string)
		if !transactionServerID.MatchString(id) || id == active || id == primary || slices.Contains(scope.backups, id) {
			return transactionEmergencyScope{}, false
		}
		scope.backups = append(scope.backups, id)
	}
	return scope, true
}

// Absence is the existing default-off state; a present file needs an explicit
// boolean. Full roster schema and catalog identity remain caller obligations.
func transactionReserveOff(image TransactionImage) bool {
	if !image.Exists {
		return true
	}
	enabled, ok := transactionReserveEnabled(image)
	return ok && !enabled
}

func transactionReserveEnabled(image TransactionImage) (bool, bool) {
	if !image.Exists {
		return false, false
	}
	v, err := strictjson.Decode(image.Data)
	m, ok := v.(map[string]any)
	if err != nil || !ok {
		return false, false
	}
	enabled, ok := m["enabled"].(bool)
	return enabled, ok
}
