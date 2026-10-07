// Package syncplan holds the one sync predicate both faces share: what a
// source→target synchronization must copy and delete. The CLI's sync
// command and the GUI's Synchronize dialog both compute their plans here,
// so the semantics can never drift between faces.
package syncplan

import "sort"

// Plan names what a source→target sync must copy and delete: a file
// copies when it is missing at the target or its size differs — the
// size-equal pair is unchanged, and mtimes are not consulted because
// clocks lie across machines — and, when del is set, a target file
// absent at the source is named for deletion. Both lists come back
// sorted; size-equal pairs count as skipped. del=false never names a
// deletion (the caller decides whether the delete leg runs; the plan
// only ever reports it when asked).
func Plan(source, target map[string]int64, del bool) (copies, dels []string, skipped int) {
	copies = make([]string, 0, len(source))
	for rel, size := range source {
		if ts, ok := target[rel]; ok && ts == size {
			skipped++
			continue
		}
		copies = append(copies, rel)
	}
	if del {
		dels = make([]string, 0)
		for rel := range target {
			if _, ok := source[rel]; !ok {
				dels = append(dels, rel)
			}
		}
		sort.Strings(dels)
	}
	sort.Strings(copies)
	return copies, dels, skipped
}
