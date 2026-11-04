//go:build production

package platform

// searchDirOverride is empty in packages: the environment must not be able to
// replace the executable worker.
func searchDirOverride() string {
	return ""
}
