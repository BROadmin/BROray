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
		key, cont, rotation, id string
		rec                     servers.Record
	}
	makeEntry := func(rec servers.Record) (entry, bool) {
		id := jqRaw(rec["id"])
		key, e1 := servers.CanonicalKey(rec)
		cont, e2 := servers.ContinuityKey(rec)
		rot, e3 := servers.RotationKey(rec)
		return entry{key, cont, rot, id, rec}, safeFileID(id) && e1 == nil && e2 == nil && e3 == nil
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
			row.id, row.matchKey = oldE[chosen].id, oldE[chosen].key
			for _, j := range ms {
				row.previousKeys = append(row.previousKeys, oldE[j].key)
			}
		}
		if ids[row.id] {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		ids[row.id] = true
		rec := deepCopy(n.rec)
		rec["id"] = row.id
		sourceOf(rec)["importKey"] = n.key
		row.rec = rec
		out = append(out, row)
	}
	return out, ""
}
