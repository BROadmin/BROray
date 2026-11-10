package xray

import (
	"context"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	mxray "github.com/BROadmin/broray/modules/xray"
)

// pinnedTestedAt is when the pinned versions were verified (modules/xray.Pinned).
const pinnedTestedAt = "2026-11-01T00:00:00Z"

// versionKey is broray_xray_version_key: first three dot parts, non-numeric
// → 0, %04d%02d%02d.
func versionKey(v string) int {
	p := strings.Split(strings.TrimPrefix(v, "v"), ".")
	key := 0
	mult := []int{10000, 100, 1}
	for i := 0; i < 3; i++ {
		n := 0
		if i < len(p) {
			x := p[i]
			if j := strings.IndexByte(x, '-'); j >= 0 && i == 2 {
				x = x[:j]
			}
			if v, err := strconv.Atoi(x); err == nil && v >= 0 {
				n = v
			}
		}
		key += n * mult[i]
	}
	return key
}

// validTag is valid_tag of lib/xray-releases.jq.
func validTag(t string) bool {
	if len(t) > 80 || !strings.HasPrefix(t, "v") {
		return false
	}
	for _, r := range t {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '.') {
			return false
		}
	}
	p := strings.Split(t[1:], ".")
	if len(p) < 3 {
		return false
	}
	dec := func(s string, max int) bool {
		if s == "" || len(s) > max {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return dec(p[0], 4) && dec(p[1], 2) && dec(strings.Split(p[2], "-")[0], 2)
}

func releaseURL(tag, name string) string {
	return "https://github.com/XTLS/Xray-core/releases/download/" + tag + "/" + name
}

func archName() string {
	if a, err := mxray.CurrentAsset(); err == nil {
		z := strings.TrimSuffix(strings.TrimPrefix(a.Zip, "Xray-linux-"), ".zip")
		return z
	}
	return archOf("")
}

// handleUpdateCheck is X6: the official XTLS catalog (2 stable + 5
// pre-releases, plus the installed and the tested version) with the
// compatibility of each archive for this BROray build.
func (s *service) handleUpdateCheck(c *cgi.Req) (any, error) {
	data, details := s.updateCheck(c.R.Context())
	if data == nil {
		return nil, cgi.Ed(502, "XRAY_UPDATE_CHECK_FAILED", "Проверка обновления Xray завершилась ошибкой.", details)
	}
	return data, nil
}

func (s *service) updateCheck(ctx context.Context) (map[string]any, string) {
	bin := s.d.Root.XrayBinary()
	current := installedVersion(s.versionOutput(ctx, bin))
	// 3.5.0: Xray is not shipped inside the package (3.x bundled it), so a
	// missing binary is a normal first-install state, not a failure.
	missing := current == ""
	if _, err := mxray.CurrentAsset(); err != nil {
		return nil, "Архитектура устройства не поддерживается этим пакетом BROray."
	}
	catalog, complete, err := s.fetchCatalog(ctx, current)
	if err != nil {
		return nil, "Не удалось получить список официальных релизов Xray."
	}
	var latest *release
	releases := make([]any, 0, len(catalog)+1)
	installedSeen := false
	for i := range catalog {
		r := catalog[i]
		if latest == nil && !r.Prerelease {
			latest = &catalog[i]
		}
		row := r.row(current)
		if row["installed"] == true {
			installedSeen = true
		}
		releases = append(releases, row)
	}
	if latest == nil {
		return nil, "Не найден официальный архив Xray для этой архитектуры."
	}
	if !installedSeen && !missing {
		releases = append(releases, map[string]any{"tagName": "v" + current, "version": current, "installed": true, "available": false,
			"prerelease": nil, "compatibility": map[string]any{"status": "untested", "label": "Не проверялась на совместимость с BROray"}})
	}
	latestTag := latest.Tag
	curKey, latestKey := versionKey(current), versionKey(latestTag)
	var curSize int64
	if st, ok := regular(bin); ok {
		curSize = st.Size()
	}
	resources, resourceErr := s.storageResources()
	optFree, tmpFree := resources.FlashFree, resources.RAMFree
	optRequired := xrayStorageRequired(curSize, latest.Zip.Size)
	storage := map[string]any{"freeBytes": optFree, "requiredBytes": optRequired, "shortfallBytes": maxInt(0, optRequired-optFree), "ok": optFree >= optRequired}
	tmpRequired := latest.Zip.Size + mxray.MemoryReserve
	reinstallAllowed := tmpFree >= tmpRequired && optFree >= optRequired
	temporary := map[string]any{"kind": "ram", "reserveBytes": mxray.MemoryReserve, "verified": resourceErr == nil, "freeBytes": tmpFree, "requiredBytes": tmpRequired, "shortfallBytes": maxInt(0, tmpRequired-tmpFree), "reinstallAllowed": reinstallAllowed && resourceErr == nil,
		"lowSpaceRequiredBytes": tmpRequired + curSize, "lowSpaceAllowed": resourceErr == nil && tmpFree >= tmpRequired+curSize && optFree >= installReserve}
	storage["estimate"] = true
	storage["exactAfterDownload"] = true
	message := "Установлена актуальная версия Xray."
	switch {
	case optFree < optRequired:
		message = "Недостаточно свободного места в /opt для безопасной установки Xray."
	case tmpFree < tmpRequired:
		message = "Недостаточно временной памяти для безопасной установки Xray."
	case missing:
		message = "Xray не установлен. Выберите версию и нажмите «Установить»."
	case latestKey > curKey:
		message = "Доступна новая версия Xray."
	case curKey > latestKey:
		message = "Установленная версия новее последнего стабильного релиза."
	}
	latestRow := latest.row(current)
	return map[string]any{
		"success": true, "currentVersion": current, "latestVersion": strings.TrimPrefix(latestTag, "v"), "latestTag": latestTag,
		"channel": "stable", "prerelease": false, "publishedAt": latest.Published, "updateAvailable": latestKey > curKey, "installedNewer": curKey > latestKey,
		"asset": latestRow["asset"], "digest": latestRow["digest"], "archiveSha256": latest.SHA256,
		"compatibility": latestRow["compatibility"], "storage": storage, "temporaryStorage": temporary, "message": message,
		"context": map[string]any{"candidateId": app.Version, "releaseId": app.Version, "version": app.Version, "architecture": archName(),
			"sourceSha256": map[string]any{}},
		"catalogComplete": complete, "catalogSource": "github", "releases": releases,
	}, ""
}

// downloadEstimate is only a conservative unknown-candidate estimate for the
// catalog. Execution uses the verified ZIP member size, never this estimate.
const downloadEstimate = 16 << 20

// Keep the catalog preflight at least as conservative as the install guard.
const installReserve = 8 << 20

func xrayStorageRequired(currentSize, archiveSize int64) int64 {
	return maxInt(currentSize, archiveSize) + installReserve
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// installedVersion is `sed -n '1s/^Xray \([^ ]*\).*/\1/p'` of `xray version`.
func installedVersion(out string) string {
	line := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(line, "Xray ") {
		return ""
	}
	f := strings.SplitN(line[5:], " ", 2)
	return f[0]
}
