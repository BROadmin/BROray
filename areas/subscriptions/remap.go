package subscriptions

import (
	"sort"

	"github.com/BROadmin/broray/areas/servers"
)

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// remap first reconciles a unique SNI/ShortID variant (including accumulated
// historical copies), then exact configurations, then unique continuity.
// The caller supplies only records owned by one subscription. All lineage is
// returned so partial responses cannot retain superseded copies as new nodes.
func (s *svc) remap(olds []oldRow, stage []servers.Record) ([]newRow, string) {
	type entry struct {
		key, storedKey, cont, rotation, id string
		rec                                servers.Record
	}
	makeEntry := func(rec servers.Record) (entry, bool) {
		id := jqRaw(rec["id"])
		original, err := servers.DPIOriginalRecord(rec)
		if err != nil {
			return entry{}, false
		}
		key, e1 := servers.CanonicalKey(original)
		storedKey, e0 := servers.CanonicalKey(rec)
		cont, e2 := servers.ContinuityKey(original)
		rot, e3 := servers.RotationKey(original)
		return entry{key, storedKey, cont, rot, id, rec}, safeFileID(id) && e0 == nil && e1 == nil && e2 == nil && e3 == nil
	}
	var oldE, newE []entry
	for _, old := range olds {
		e, ok := makeEntry(old.file.rec)
		if !ok || servers.Validate(old.file.rec, old.file.path) != nil || old.file.id != e.id {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		oldE = append(oldE, e)
	}
	for _, rec := range stage {
		if _, local := rec["dpi"]; local {
			return nil, "SERVER_SYNC_DPI_CONFLICT"
		}
		e, ok := makeEntry(rec)
		if !ok || jqRaw(sourceOf(rec)["importKey"]) == "" {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		newE = append(newE, e)
	}
	sort.SliceStable(oldE, func(i, j int) bool { return oldE[i].id < oldE[j].id })
	sort.SliceStable(newE, func(i, j int) bool { return newE[i].id < newE[j].id })
	newRot := map[string]int{}
	newKeys := map[string]bool{}
	for _, n := range newE {
		if newKeys[n.key] {
			return nil, "SERVER_SYNC_AMBIGUOUS_IDENTITY"
		}
		newKeys[n.key] = true
		newRot[n.rotation]++
	}
	matches := make([][]int, len(newE))
	used := map[int]bool{}
	for i, n := range newE {
		if newRot[n.rotation] == 1 {
			for j, o := range oldE {
				if o.rotation == n.rotation {
					matches[i] = append(matches[i], j)
					used[j] = true
				}
			}
		}
	}
	for i, n := range newE {
		if len(matches[i]) > 0 {
			continue
		}
		for j, o := range oldE {
			if !used[j] && o.key == n.key {
				matches[i] = append(matches[i], j)
				used[j] = true
			}
		}
	}
	oldCont, newCont := map[string][]int{}, map[string][]int{}
	for j, o := range oldE {
		if !used[j] {
			oldCont[o.cont] = append(oldCont[o.cont], j)
		}
	}
	for i, n := range newE {
		if len(matches[i]) == 0 {
			newCont[n.cont] = append(newCont[n.cont], i)
		}
	}
	for cont, ni := range newCont {
		oi := oldCont[cont]
		if len(oi) == 0 {
			continue
		}
		if len(oi) != 1 || len(ni) != 1 {
			return nil, "SERVER_SYNC_AMBIGUOUS_IDENTITY"
		}
		matches[ni[0]] = oi
	}
	active := servers.ActiveID(s.d)
	out := make([]newRow, 0, len(newE))
	ids := map[string]bool{}
	for i, n := range newE {
		row := newRow{key: n.key, cont: n.cont, id: n.id, matchKey: n.key}
		ms := matches[i]
		var previous servers.Record
		if len(ms) > 0 {
			chosen := ms[0]
			for _, j := range ms {
				if oldE[j].key == n.key {
					chosen = j
					break
				}
			}
			for _, j := range ms {
				if oldE[j].id == active {
					chosen = j
					break
				}
			}
			// Matching uses original source identity; cleanup still refers to
			// the actual saved keys. A merged lineage cannot choose between
			// conflicting local choices, even when one copy is active.
			_, chosenDPI := oldE[chosen].rec["dpi"]
			for _, j := range ms {
				_, localDPI := oldE[j].rec["dpi"]
				if localDPI != chosenDPI {
					return nil, "SERVER_SYNC_AMBIGUOUS_IDENTITY"
				}
			}
			previous = oldE[chosen].rec
			row.id, row.matchKey = oldE[chosen].id, oldE[chosen].storedKey
			for _, j := range ms {
				row.previousKeys = append(row.previousKeys, oldE[j].storedKey)
			}
		}
		if ids[row.id] {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		ids[row.id] = true
		rec := deepCopy(n.rec)
		if previous != nil {
			var err error
			rec, err = servers.RefreshDPIRecord(previous, rec)
			if err != nil {
				return nil, "SERVER_SYNC_DPI_CONFLICT"
			}
		}
		rec["id"] = row.id
		sourceOf(rec)["importKey"] = n.key
		row.rec = rec
		out = append(out, row)
	}
	return out, ""
}
