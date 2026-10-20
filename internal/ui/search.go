package ui

import (
	"fmt"
	"strings"

	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/viscada"
)

// searchModes: fuzzy finds every keyword, ignoring case, in the NodeID, name
// or browse path; exact matches a whole NodeID, viSCADA 属性标识, identifier
// or name.
var searchModes = []string{"模糊", "精确"}

// pointQuery selects result rows: the quality filter, then the search.
type pointQuery struct {
	filter, search string
	exact          bool
}

func (d *Desktop) query() pointQuery {
	return pointQuery{filter: d.resultFilter.Selected, search: strings.TrimSpace(d.search.Text), exact: d.searchMode.Selected == "精确"}
}

// label names the query in export titles and messages.
func (q pointQuery) label() string {
	if q.search == "" {
		return q.filter
	}
	mode := "模糊"
	if q.exact {
		mode = "精确"
	}
	return fmt.Sprintf("%s · %s搜索“%s”", q.filter, mode, q.search)
}

// matcher compiles the query once for a pass over many rows.
func (q pointQuery) matcher() func(*device.ReadResult) bool {
	filter := q.filter
	if q.search == "" {
		return func(r *device.ReadResult) bool { return matchesPoint(r, filter) }
	}
	match := matchNode(q.search, q.exact)
	return func(r *device.ReadResult) bool {
		if !matchesPoint(r, filter) {
			return false
		}
		if q.exact {
			return match(r.Address.NodeID, r.DisplayName)
		}
		return match(r.Address.NodeID, r.DisplayName, strings.Join(r.Path, "/"))
	}
}

// matchNode matches a node by its fields, the NodeID first. Fuzzy keywords
// may each match a different field: 热力站 温度 finds a point whose path holds
// one and whose name the other. Exact search also takes a NodeID typed another
// way, such as ns＝2；s＝Tag, or a viSCADA 属性标识, finding the node it names,
// and a bare identifier such as Tag.
func matchNode(search string, exact bool) func(fields ...string) bool {
	match := device.Matcher(search, exact)
	if !exact {
		return match
	}
	node := ""
	if a, _, err := parsePoint(search, "Auto", 1); err == nil {
		node = a.NodeID
	}
	return func(fields ...string) bool {
		return fields[0] == node || match(fields...) || match(device.Identifier(fields[0]))
	}
}

// parsePoint reads a NodeID, or a viSCADA 属性标识 such as 2::s::Tag::LREAL,
// which carries its own type; viscadaID reports the latter.
func parsePoint(text string, kind device.DataType, count int) (a device.Address, viscadaID bool, err error) {
	if viscada.IsIdentifier(text) {
		a, err = viscada.ParseIdentifier(text)
		return a, true, err
	}
	a, err = device.ParseAddress(text, kind, count)
	return a, false, err
}
