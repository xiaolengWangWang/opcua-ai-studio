package ui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
)

// pointTree indexes the visible rows as a tree. Branch IDs join their path
// with NUL; leaf IDs are "#" and the result index.
type pointTree struct {
	children map[widget.TreeNodeID][]widget.TreeNodeID
	rows     map[widget.TreeNodeID]int    // leaf → result index
	names    map[widget.TreeNodeID]string // node → label
	points   map[widget.TreeNodeID]int    // branch → points below it
}

// treePath places a point: below its browse path when a full read recorded
// one, else below its namespace and the parts of a string identifier split at
// . / and \, as Kepware, Siemens and many Chinese servers name tags.
func treePath(r *device.ReadResult) (path []string, name string) {
	ns, id := "ns=0", r.Address.NodeID
	if strings.HasPrefix(id, "ns=") {
		i := strings.IndexByte(id, ';')
		ns, id = id[:i], id[i+1:]
	}
	parts := []string{id}
	if s, ok := strings.CutPrefix(id, "s="); ok {
		if p := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '/' || r == '\\' }); len(p) > 0 {
			parts = p
		}
	}
	name = r.DisplayName
	if name == "" {
		name = parts[len(parts)-1]
	}
	if r.Path != nil {
		return r.Path, name
	}
	return append([]string{ns}, parts[:len(parts)-1]...), name
}

func buildTree(results []device.ReadResult, visible []int) pointTree {
	t := pointTree{children: map[string][]string{}, rows: make(map[string]int, len(visible)), names: make(map[string]string, len(visible)), points: map[string]int{}}
	for _, i := range visible {
		path, name := treePath(&results[i])
		parent := ""
		for _, part := range path {
			id := parent + "\x00" + part
			if _, ok := t.names[id]; !ok {
				t.names[id] = part
				t.children[parent] = append(t.children[parent], id)
			}
			t.points[id]++
			parent = id
		}
		leaf := "#" + strconv.Itoa(i)
		t.rows[leaf] = i
		t.names[leaf] = name
		t.children[parent] = append(t.children[parent], leaf)
	}
	return t
}

// newPointTree shows each branch with its point count and each point with
// its value colored by quality, then type, access, quality and NodeID.
func (d *Desktop) newPointTree() *widget.Tree {
	t := widget.NewTree(
		func(id widget.TreeNodeID) []widget.TreeNodeID { return d.treeIndex.children[id] },
		func(id widget.TreeNodeID) bool { _, leaf := d.treeIndex.rows[id]; return !leaf },
		func(bool) fyne.CanvasObject {
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			detail := widget.NewLabel("")
			detail.Importance = widget.LowImportance
			return container.NewHBox(name, widget.NewLabel(""), detail)
		},
		func(id widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
			labels := o.(*fyne.Container).Objects
			name, value, detail := labels[0].(*widget.Label), labels[1].(*widget.Label), labels[2].(*widget.Label)
			name.SetText(d.treeIndex.names[id])
			if branch {
				value.Importance = widget.LowImportance
				value.SetText(fmt.Sprintf("%d 个点", d.treeIndex.points[id]))
				detail.SetText("")
				return
			}
			i, ok := d.treeIndex.rows[id]
			if !ok || i >= len(d.snapshot.Results) {
				return
			}
			r := &d.snapshot.Results[i]
			row := pointRow(r)
			value.Importance = qualityImportance(r.Quality)
			if row[4] != "" {
				value.SetText("= " + row[4])
			} else {
				value.SetText(r.Quality)
			}
			detail.SetText(strings.Join(nonEmpty(row[2], row[3], row[5], r.Error, r.Address.NodeID), " · "))
		})
	t.OnSelected = func(id widget.TreeNodeID) {
		if i, ok := d.treeIndex.rows[id]; ok && i < len(d.snapshot.Results) {
			d.selectResult(d.snapshot.Results[i])
		}
	}
	return t
}

func nonEmpty(ss ...string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// refreshTree rebuilds the tree from the visible rows, only while it shows:
// indexing 50000 points on every refresh would slow the table view.
func (d *Desktop) refreshTree() {
	if d.tree == nil || !d.tree.Visible() {
		return
	}
	fresh := len(d.treeIndex.rows) == 0
	d.treeIndex = buildTree(d.snapshot.Results, d.visible)
	// Leaf IDs index the results, which a new list replaces.
	d.tree.UnselectAll()
	// Open the top level of a small new tree, so a demo or a short list shows
	// its points; later refreshes keep what the user opened or closed.
	if fresh && len(d.visible) <= 200 {
		for _, id := range d.treeIndex.children[""] {
			d.tree.OpenBranch(id)
		}
	}
	d.tree.Refresh()
}
