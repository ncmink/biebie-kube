// Package update chooses which GitHub Release asset Biebie Kube should install.
//
// Wails' default matcher looks for GOOS and GOARCH as substrings and skips
// installer names. Our macOS updater file is a universal zip
// (darwin-universal), which contains neither arm64 nor amd64, so the default
// would miss it and could also pick a .dmg that the updater cannot apply.
package update

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// MatchAsset picks the zip the running platform can swap into place.
//
// Preference order:
//   - darwin: *-darwin-universal.zip, then *-darwin-{arch}.zip
//   - windows: *-windows-{arch}.zip
//
// Installers, disk images and checksum sidecars are never selected.
func MatchAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	for _, suffix := range suffixes(req.Platform, req.Arch) {
		for i, asset := range assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), suffix) {
				return i
			}
		}
	}
	return -1
}

func suffixes(platform, arch string) []string {
	plat := strings.ToLower(platform)
	arch = strings.ToLower(arch)
	switch plat {
	case "darwin":
		return []string{"darwin-universal.zip", "darwin-" + arch + ".zip"}
	case "windows":
		return []string{"windows-" + arch + ".zip"}
	default:
		if plat == "" || arch == "" {
			return nil
		}
		return []string{plat + "-" + arch + ".zip"}
	}
}
