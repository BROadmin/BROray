package ops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestBackupStagingCarriesActualFence(t *testing.T) {
	a := backupArea(t)
	setFake(t, a, "original")
	file := exportFile(t, "")
	setFake(t, a, "changed")
	part := fakePart(a)
	original := part.Stage
	checks := 0
	part.Stage = func(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
		checks++
		holder, held := a.d.Lock.Current()
		if !held || holder.Pending || !compat.FenceHeld(ctx) {
			t.Fatal("backup staging lacks actual fenced context")
		}
		return original(ctx, raw, st)
	}
	a.parts = func() []compat.BackupPart { return []compat.BackupPart{part, a.settingsPart()} }
	pv := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": file}), backupHdr())
	if pv.Code != 200 {
		t.Fatal(pv.Code, pv.Raw)
	}
	token := data(pv)["confirmToken"]
	im := call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "confirmToken": token}), backupHdr())
	if im.Code != 200 || checks != 2 {
		t.Fatal(im.Code, im.Raw, checks)
	}
}

func TestBackupManagedGeodataPathScope(t *testing.T) {
	if !copyPathAllowed("runtime/broray-geodata") {
		t.Fatal("managed geodata cannot be recovered/undone")
	}
	for _, path := range []string{"runtime", "runtime/xray", "runtime/broray-geodata-other", "runtime/broray-geodata/foreign", "runtime/broray-geodata/../xray"} {
		if copyPathAllowed(path) {
			t.Fatal("unrelated runtime path allowed", path)
		}
	}
}
