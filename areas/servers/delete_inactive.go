package servers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

type inactiveImage struct {
	path, hash string
	info       os.FileInfo
	parentInfo os.FileInfo
}

func (s *svc) handleDeleteInactive(c *cgi.Req) (any, error) {
	if c.Header("X-BROray-Request") != "operations" {
		return nil, cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие.")
	}
	if !compat.OriginAllowed(c.R) {
		return nil, cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён.")
	}
	if err := bodyCheck(c); err != nil {
		return nil, err
	}
	v, err := strictjson.Decode(c.Body())
	body, ok := v.(map[string]any)
	if err != nil || !ok || len(body) != 1 {
		return nil, cgi.E(400, "INVALID_BODY", "Укажите текущий активный сервер.")
	}
	active, ok := body["activeId"].(string)
	if !ok || !ValidID(active) {
		return nil, cgi.E(400, "INVALID_BODY", "Укажите текущий активный сервер.")
	}
	return s.deleteInactive(active)
}

func readInactiveImage(path string, optional bool) (inactiveImage, []byte, error) {
	parent, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		if optional && errors.Is(err, os.ErrNotExist) {
			return inactiveImage{path: path}, nil, nil
		}
		return inactiveImage{}, nil, err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil {
		return inactiveImage{}, nil, err
	}
	info, err := os.Lstat(path)
	if optional && os.IsNotExist(err) {
		return inactiveImage{path: path, parentInfo: parentInfo}, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return inactiveImage{}, nil, fmt.Errorf("ambiguous server entry")
	}
	raw, err := reserveRuntimeFile(path, 1<<20)
	if err != nil {
		return inactiveImage{}, nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || !reserveProbeDirectoryMatches(filepath.Dir(path), parent) {
		return inactiveImage{}, nil, fmt.Errorf("server entry changed")
	}
	return inactiveImage{path: path, hash: compat.SHA256Hex(raw), info: after, parentInfo: parentInfo}, raw, nil
}

func (entry inactiveImage) unchanged() bool {
	current, _, err := readInactiveImage(entry.path, entry.info == nil)
	if err != nil || current.hash != entry.hash {
		return false
	}
	if (current.parentInfo == nil) != (entry.parentInfo == nil) {
		return false
	}
	if entry.parentInfo != nil && !os.SameFile(entry.parentInfo, current.parentInfo) {
		return false
	}
	if entry.info == nil {
		return current.info == nil
	}
	return current.info != nil && os.SameFile(entry.info, current.info)
}

func (s *svc) deleteInactive(expectedActive string) (any, error) {
	return s.job("delete-inactive", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		if !ValidID(expectedActive) {
			return nil, cgi.E(409, "ACTIVE_SERVER_CHANGED", "Активный сервер не определён. Обновите страницу.")
		}
		if err := s.ordinarySelectionAllowed(); errors.Is(err, errReserveModeActive) {
			return nil, cgi.E(409, "RESERVE_MODE_ACTIVE", "Для удаления остальных серверов сначала выключите резервирование VPN.")
		} else if err != nil {
			return nil, cgi.E(409, "RESERVE_PREFERENCES_UNCONFIRMED", "Не удалось прочитать настройки резервирования. Удаление не начато.")
		}
		activeRaw, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
		if err != nil || strings.TrimSpace(string(activeRaw)) != expectedActive {
			return nil, cgi.E(409, "ACTIVE_SERVER_CHANGED", "Активный сервер изменился. Обновите страницу и повторите действие.")
		}
		ids, err := RecordIDs(s.d)
		if err != nil {
			return nil, opFailed("Не удалось прочитать список серверов.")
		}
		type removal struct {
			id              string
			record, quality inactiveImage
		}
		var entries []removal
		var active inactiveImage
		for _, id := range ids {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !ValidID(id) {
				return nil, opFailed("Список содержит неоднозначную запись. Удаление не начато.")
			}
			entry, raw, err := readInactiveImage(RecordPath(s.d, id), false)
			if err != nil {
				return nil, opFailed("Не удалось подтвердить запись сервера. Удаление не начато.")
			}
			value, err := strictjson.Decode(raw)
			record, ok := value.(map[string]any)
			if err != nil || !ok || record["id"] != id || Validate(Record(record), filepath.Base(entry.path)) != nil {
				return nil, opFailed("Список содержит повреждённую запись. Удаление не начато.")
			}
			if id == expectedActive {
				active = entry
				continue
			}
			quality, _, err := readInactiveImage(QualityPath(s.d, id), true)
			if err != nil {
				return nil, opFailed("Не удалось подтвердить файл проверки сервера. Удаление не начато.")
			}
			entries = append(entries, removal{id, entry, quality})
		}
		if active.info == nil {
			return nil, cgi.E(409, "ACTIVE_SERVER_CHANGED", "Запись активного сервера не найдена. Удаление не начато.")
		}
		op.SetCancelability("protected")
		op.Phase("committing")
		removed := 0
		incomplete := func() (any, error) {
			return nil, cgi.E(409, "SERVER_BULK_DELETE_INCOMPLETE", fmt.Sprintf("Удалено записей: %d. Состояние изменилось или удаление не удалось; остальные записи сохранены. Обновите список.", removed))
		}
		for _, entry := range entries {
			now, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
			if err != nil || !bytes.Equal(now, activeRaw) || !active.unchanged() || !entry.record.unchanged() || !entry.quality.unchanged() {
				return incomplete()
			}
			err = platform.RemoveRegularNoFollow(entry.record.path)
			if err == nil && entry.quality.info != nil {
				err = platform.RemoveRegularNoFollow(entry.quality.path)
			}
			if err != nil {
				if _, statErr := os.Lstat(entry.record.path); os.IsNotExist(statErr) {
					removed++
				}
				return incomplete()
			}
			removed++
		}
		return map[string]any{"removed": removed, "activeId": expectedActive, "updatedAt": compat.Stamp(s.now())}, nil
	})
}
