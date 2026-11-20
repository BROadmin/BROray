package subscriptions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"golang.org/x/sys/unix"
)

// The display scanner deliberately tolerates bad entries. Destructive source
// operations must instead prove the complete catalog before the first unlink.
type deletionImage struct {
	path         string
	raw          []byte
	info, parent os.FileInfo
}

func readDeletionImage(path string, optional bool) (deletionImage, error) {
	image := deletionImage{path: path}
	parent, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		if optional && errors.Is(err, os.ErrNotExist) {
			return image, nil
		}
		return image, err
	}
	defer parent.Close()
	image.parent, err = parent.Stat()
	if err != nil {
		return image, err
	}
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if optional && errors.Is(err, os.ErrNotExist) {
			return image, nil
		}
		return image, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	image.info, err = f.Stat()
	if err != nil {
		return image, err
	}
	if !image.info.Mode().IsRegular() || image.info.Size() > 1<<20 {
		return image, fmt.Errorf("ambiguous deletion entry")
	}
	image.raw, err = io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return image, err
	}
	if len(image.raw) > 1<<20 {
		return image, fmt.Errorf("deletion entry too large")
	}
	after, err := f.Stat()
	if err != nil {
		return image, err
	}
	if after.Size() != image.info.Size() || !after.ModTime().Equal(image.info.ModTime()) {
		return image, fmt.Errorf("deletion entry changed")
	}
	return image, nil
}

func (image deletionImage) unchanged() bool {
	now, err := readDeletionImage(image.path, image.info == nil)
	if err != nil || !bytes.Equal(image.raw, now.raw) {
		return false
	}
	if (image.parent == nil) != (now.parent == nil) {
		return false
	}
	if image.parent != nil && !os.SameFile(image.parent, now.parent) {
		return false
	}
	if image.info == nil {
		return now.info == nil
	}
	return now.info != nil && os.SameFile(image.info, now.info)
}

type deletionMember struct {
	id              string
	record, quality deletionImage
}

func (s *svc) deletionMembers(subID string) ([]deletionMember, error) {
	var members []deletionMember
	foreignIDs := map[string]bool{}
	for _, path := range []string{servers.ServersDir(s.d), s.disabledDir(subID)} {
		dir, err := platform.OpenDirNoFollow(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries, err := dir.ReadDir(-1)
		closeErr := dir.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
				continue
			}
			image, err := readDeletionImage(filepath.Join(path, name), false)
			if err != nil {
				return nil, err
			}
			if _, err := strictjson.Decode(image.raw); err != nil {
				return nil, err
			}
			rec, ok := decodeObject(image.raw)
			if !ok {
				return nil, fmt.Errorf("invalid catalog record")
			}
			if !belongsTo(rec, subID) {
				foreignIDs[strings.TrimSuffix(name, ".json")] = true
				if id, ok := rec["id"].(string); ok {
					foreignIDs[id] = true
				}
				continue
			}
			if err := servers.Validate(rec, name); err != nil {
				return nil, err
			}
			id := strings.TrimSuffix(name, ".json")
			if rec["id"] != id {
				return nil, fmt.Errorf("catalog record identity mismatch")
			}
			quality, err := readDeletionImage(servers.QualityPath(s.d, id), true)
			if err != nil {
				return nil, err
			}
			members = append(members, deletionMember{id, image, quality})
		}
	}
	for _, member := range members {
		if foreignIDs[member.id] {
			return nil, fmt.Errorf("server identity belongs to multiple sources")
		}
	}
	return members, nil
}

func (s *svc) removeServers(subID string, guards ...func() bool) (int, error) {
	if err := s.syncMarkers(); err != nil {
		return 0, err
	}
	refuse := func() error {
		return fail("SERVER_SOURCE_REMOVE_FAILED", "Не удалось безопасно прочитать все записи серверов подписки. Удаление отменено.")
	}
	allowed := func() bool {
		for _, guard := range guards {
			if guard == nil || !guard() {
				return false
			}
		}
		return true
	}
	if !allowed() {
		return 0, refuse()
	}
	active, err := readDeletionImage(servers.ActiveFile(s.d), true)
	if err != nil {
		return 0, refuse()
	}
	activeID := strings.TrimSpace(string(active.raw))
	if activeID != "" && !servers.ValidID(activeID) {
		return 0, refuse()
	}
	members, err := s.deletionMembers(subID)
	if err != nil {
		return 0, refuse()
	}
	for _, member := range members {
		if member.id == activeID {
			return 0, fail("ACTIVE_SERVER_CONFLICT", "Нельзя удалить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер.")
		}
		if !allowed() || !member.record.unchanged() || !member.quality.unchanged() {
			return 0, refuse()
		}
	}
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.id)
	}
	if err := servers.CheckSourceRemovalLegacy(s.d.Ctx, s.d, ids); err != nil {
		if errors.Is(err, servers.ErrSourceRemovalQueueRequired) {
			return 0, fail("SOURCE_REMOVAL_QUEUE_REQUIRED", "Для удаления этого источника требуется защищённая операция. Обновите страницу и повторите удаление.")
		}
		return 0, refuse()
	}
	count := 0
	for _, member := range members {
		if !allowed() || !active.unchanged() || !member.record.unchanged() || !member.quality.unchanged() {
			return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Состояние серверов изменилось во время удаления. Подписка сохранена; обновите список.")
		}
		if err := platform.RemoveRegularNoFollow(member.record.path); err != nil {
			return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Не удалось удалить сервер подписки. Подписка сохранена.")
		}
		count++
	}
	removedQuality := map[string]bool{}
	for _, member := range members {
		if removedQuality[member.quality.path] {
			continue
		}
		if member.quality.info != nil {
			if !allowed() || !active.unchanged() || !member.quality.unchanged() {
				return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Результаты проверки сервера изменились. Подписка сохранена.")
			}
			if err := platform.RemoveRegularNoFollow(member.quality.path); err != nil {
				return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Не удалось удалить результаты проверки сервера. Подписка сохранена.")
			}
			removedQuality[member.quality.path] = true
		}
	}
	// Only an empty owned directory can be removed; foreign entries are retained.
	_ = os.Remove(s.disabledDir(subID))
	return count, nil
}
