//go:build !production

package platform

import "os"

// searchDirOverride returns SMARTNOTE_SEARCH_DIR, the directory with the
// worker, the ONNX Runtime library and the model subdirectory, for development.
func searchDirOverride() string {
	return os.Getenv("SMARTNOTE_SEARCH_DIR")
}
