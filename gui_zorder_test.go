//go:build !s3b_headless

package gui

import (
	"reflect"
	"testing"
)

// orderGroup is the whole bookkeeping heart of the window-group raise: it
// must keep the siblings' relative stacking stable across raises, promote
// the focused window to the top slot, forget closed windows and take new
// ones in at the bottom — deterministically, because a shuffle here would
// visibly reorder the popouts on every click.
func TestOrderGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		order   []string
		live    []string
		focused string
		want    []string
	}{
		{
			name:    "fresh group: focused top, then creation order",
			order:   nil,
			live:    []string{"main", "popout:a", "popout:b"},
			focused: "main",
			want:    []string{"main", "popout:a", "popout:b"},
		},
		{
			name:    "clicking a sibling promotes it, others keep their order",
			order:   []string{"main", "popout:a", "popout:b"},
			live:    []string{"main", "popout:a", "popout:b"},
			focused: "popout:b",
			want:    []string{"popout:b", "main", "popout:a"},
		},
		{
			name:    "closed window is forgotten",
			order:   []string{"main", "popout:a", "popout:b"},
			live:    []string{"main", "popout:b"},
			focused: "main",
			want:    []string{"main", "popout:b"},
		},
		{
			name:    "first-seen window joins at the bottom",
			order:   []string{"main", "popout:a"},
			live:    []string{"main", "popout:a", "popout:c"},
			focused: "popout:a",
			want:    []string{"popout:a", "main", "popout:c"},
		},
		{
			name:    "focused window unknown to the order still takes the top",
			order:   []string{"popout:a", "popout:b"},
			live:    []string{"popout:a", "popout:b", "popout:c"},
			focused: "popout:c",
			want:    []string{"popout:c", "popout:a", "popout:b"},
		},
		{
			name:    "focused window not live (filtered out) leaves the order alone",
			order:   []string{"main", "popout:a"},
			live:    []string{"main", "popout:a"},
			focused: "ghost",
			want:    []string{"main", "popout:a"},
		},
		{
			name:    "stale order entries and duplicates collapse",
			order:   []string{"x", "main", "x", "popout:a"},
			live:    []string{"main", "popout:a"},
			focused: "popout:a",
			want:    []string{"popout:a", "main"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := orderGroup(tc.order, tc.live, tc.focused)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("orderGroup(%v, %v, %q) = %v, want %v",
					tc.order, tc.live, tc.focused, got, tc.want)
			}
		})
	}
}
