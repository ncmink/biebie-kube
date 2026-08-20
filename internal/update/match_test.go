package update

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestMatchAssetPrefersDarwinUniversalZip(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "SHA256SUMS"},
		{Name: "biebie-kube-v0.2.0-macos-universal.dmg"},
		{Name: "biebie-kube-v0.2.0-darwin-universal.zip"},
	}

	got := MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if got != 2 {
		t.Fatalf("darwin/arm64 picked %d (%s), want the universal zip", got, name(assets, got))
	}

	got = MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets)
	if got != 2 {
		t.Fatalf("darwin/amd64 picked %d (%s), want the universal zip", got, name(assets, got))
	}
}

func TestMatchAssetFallsBackToDarwinArchZip(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "biebie-kube-v0.2.0-darwin-arm64.zip"},
		{Name: "biebie-kube-v0.2.0-macos-universal.dmg"},
	}

	got := MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if got != 0 {
		t.Fatalf("picked %d (%s), want the arm64 zip", got, name(assets, got))
	}

	if MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets) != -1 {
		t.Fatal("amd64 must not match a darwin-arm64 zip")
	}
}

func TestMatchAssetPrefersUniversalOverArchZip(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "biebie-kube-v0.2.0-darwin-arm64.zip"},
		{Name: "biebie-kube-v0.2.0-darwin-universal.zip"},
	}

	got := MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if got != 1 {
		t.Fatalf("picked %d (%s), want the universal zip", got, name(assets, got))
	}
}

func TestMatchAssetWindowsZipSkipsInstaller(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "biebie-kube-v0.2.0-windows-amd64-installer.exe"},
		{Name: "SHA256SUMS"},
		{Name: "biebie-kube-v0.2.0-windows-amd64.zip"},
	}

	got := MatchAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets)
	if got != 2 {
		t.Fatalf("picked %d (%s), want the windows zip", got, name(assets, got))
	}

	if MatchAsset(updater.CheckRequest{Platform: "windows", Arch: "arm64"}, assets) != -1 {
		t.Fatal("arm64 must not match a windows-amd64 zip")
	}
}

func TestMatchAssetNoSuitableAsset(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "biebie-kube-v0.2.0-macos-universal.dmg"},
		{Name: "biebie-kube-v0.2.0-windows-amd64-installer.exe"},
		{Name: "SHA256SUMS"},
	}

	if got := MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets); got != -1 {
		t.Fatalf("darwin picked %d (%s), want none", got, name(assets, got))
	}
	if got := MatchAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets); got != -1 {
		t.Fatalf("windows picked %d (%s), want none", got, name(assets, got))
	}
}

func name(assets []github.ReleaseAsset, i int) string {
	if i < 0 || i >= len(assets) {
		return "<none>"
	}
	return assets[i].Name
}
