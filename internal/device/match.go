package device

import "strings"

// Matcher returns whether any of a point's fields match a search. Fuzzy
// search finds every space-separated keyword, ignoring case, in some field;
// exact search matches one whole field. An empty search matches everything.
// As in ParseAddress, a typed ns＝2；s＝ finds ns=2;s=.
func Matcher(search string, exact bool) func(fields ...string) bool {
	search = halfWidthFields(strings.TrimSpace(search))
	if exact {
		if search == "" {
			return func(...string) bool { return true }
		}
		return func(fields ...string) bool {
			for _, f := range fields {
				if f == search {
					return true
				}
			}
			return false
		}
	}
	keywords := strings.Fields(strings.ToLower(search))
	return func(fields ...string) bool {
	next:
		for _, k := range keywords {
			for _, f := range fields {
				if strings.Contains(strings.ToLower(f), k) {
					continue next
				}
			}
			return false
		}
		return true
	}
}

// Identifier is the identifier part of a NodeID: Counter for ns=2;s=Counter.
func Identifier(nodeID string) string {
	if strings.HasPrefix(nodeID, "ns=") {
		if i := strings.IndexByte(nodeID, ';'); i >= 0 {
			nodeID = nodeID[i+1:]
		}
	}
	if len(nodeID) > 1 && nodeID[1] == '=' {
		return nodeID[2:]
	}
	return nodeID
}
