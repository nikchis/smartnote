//go:build production

package platform

import "testing"

func TestSearchDirOverrideProduction(t *testing.T) {
	t.Setenv("SMARTNOTE_SEARCH_DIR", "/opt/search")

	if got := searchDirOverride(); got != "" {
		t.Errorf("searchDirOverride() = %q, want empty", got)
	}
}
