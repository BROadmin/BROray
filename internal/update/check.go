package update

import "time"

// CheckResult summarises a verified manifest for the running version
// (update-check.cgi and its cache state/update-check.json).
type CheckResult struct {
	Channel            string    `json:"channel"`
	URL                string    `json:"url"`
	CheckedAt          time.Time `json:"checkedAt"`
	Current            string    `json:"current"`
	Platform           string    `json:"platform"`
	Latest             string    `json:"latest,omitempty"`   // newest non-revoked release
	Relation           string    `json:"relation"`           // newer | same | older | none
	UpdateAvailable    bool      `json:"updateAvailable"`    // Latest can be installed over Current
	ReinstallAvailable bool      `json:"reinstallAvailable"` // Current is published for this platform
	CurrentRevoked     bool      `json:"currentRevoked"`
	Notes              string    `json:"notes,omitempty"`
	ProblemCode        string    `json:"problemCode,omitempty"` // why a newer release cannot be installed
	ProblemMessage     string    `json:"problemMessage,omitempty"`
	ManifestSHA256     string    `json:"manifestSha256"`
	KeyID              string    `json:"keyId"`
	ExpiresAt          time.Time `json:"expiresAt"`
}

func Summarize(chk *Checked, current, platformKey string, now time.Time) CheckResult {
	m := chk.Manifest
	r := CheckResult{Channel: m.Channel, URL: chk.URL, CheckedAt: now.UTC(), Current: current, Platform: platformKey,
		Relation: "none", CurrentRevoked: m.IsRevoked(current), ManifestSHA256: chk.SHA256, KeyID: chk.KeyID, ExpiresAt: m.ExpiresAt}
	if latest, ok := m.Latest(); ok {
		r.Latest, r.Notes = latest.Version, latest.Notes
		switch c := CompareVersions(latest.Version, current); {
		case c > 0:
			r.Relation = "newer"
			if _, _, err := m.Target(current, latest.Version, platformKey, false); err != nil {
				r.ProblemCode, r.ProblemMessage = Code(err), Message(err)
			} else {
				r.UpdateAvailable = true
			}
		case c == 0:
			r.Relation = "same"
		default:
			r.Relation = "older"
		}
	}
	if _, _, err := m.Target(current, "", platformKey, true); err == nil {
		r.ReinstallAvailable = true
	}
	return r
}
