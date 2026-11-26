package installstats

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"time"

	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/update"
)

type BinaryBuild struct {
	Build
	Architecture string `json:"architecture"`
}

// The archived 3.5.9-r9 binaries predate revision reporting. Only those exact
// verified executables can be identified as r9 by their SHA-256 digest.
var legacyR9 = map[string]string{
	"arm64":            "52b6617b214479f88b8e2e4cae8ac3cd8c12b38120dd23ee4b1690469d9efff2",
	"arm/7":            "08a5f7ec555454a1104e4b1e9f0f61a3a271b40f80a99044aee58486d85d1e4e",
	"mips/softfloat":   "c78fd7cde544af817e1fe845926ab521c89ffa4914d5fc57f88f56eee5b92053",
	"mipsle/softfloat": "94d52cf64ee3df63c8a1ee37bd495ce81b1af7e2ed060b564d61edb5c8f75a2a",
}

// ReadBinaryBuild reads metadata from a binary the installer has already
// identified. The supplied digest is the hash from that ownership check.
func ReadBinaryBuild(ctx context.Context, path, version, architecture, digest string) (Build, bool) {
	if !update.ValidVersion(version) || !deviceKeyPattern.MatchString(digest) {
		return Build{}, false
	}
	if version == "3.5.9" && legacyR9[architecture] == digest {
		return Build{version, "r9"}, true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version", "--build-json")
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return Build{}, false
	}
	stopRead := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stopRead()
	const maxReport = 512
	data, readErr := io.ReadAll(io.LimitReader(out, maxReport+1))
	if len(data) > maxReport || readErr != nil {
		_ = out.Close()
	}
	if err := cmd.Wait(); err != nil || readErr != nil || len(data) > maxReport {
		return Build{}, false
	}
	value, err := strictjson.Decode(data)
	fields, _ := value.(map[string]any)
	var result BinaryBuild
	if err != nil || len(fields) != 3 || fields["version"] == nil || fields["revision"] == nil || fields["architecture"] == nil ||
		json.Unmarshal(data, &result) != nil || !result.Build.valid() ||
		result.Version != version || result.Architecture != architecture {
		return Build{}, false
	}
	return result.Build, true
}
