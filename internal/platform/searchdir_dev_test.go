//go:build !production

package platform

import "testing"

func TestSearchDirOverrideDev(t *testing.T) {
	t.Setenv("SMARTNOTE_SEARCH_DIR", "/opt/search")

	if got := searchDirOverride(); got != "/opt/search" {
		t.Errorf("searchDirOverride() = %q, want /opt/search", got)
	}
}
