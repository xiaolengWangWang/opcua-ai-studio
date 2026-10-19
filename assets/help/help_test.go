package help

import (
	"strings"
	"testing"
)

// Endpoint encryption on build machines can replace files with ciphertext,
// which go:embed would ship as the in-app help.
func TestHelpIsPlainText(t *testing.T) {
	if !strings.HasPrefix(Points, "# ") || strings.Contains(Points, "E-SafeNet") {
		t.Fatalf("help is not plain Markdown: %q", Points[:min(len(Points), 40)])
	}
}
