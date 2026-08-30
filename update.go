package main

import (
	_ "embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"biebie-kube/internal/update"
)

// updateRepository is the public GitHub repo whose Releases feed the updater.
const updateRepository = "ncmink/biebie-kube"

// updaterPublicKey is the trust root for release signatures, pinned here at
// build time so a compromised or proxied release feed cannot nominate its own
// key. Its private half signs artifacts in the release workflow and never
// leaves the secret store; see cmd/sign-release.
//
//go:embed updater-key.pem
var updaterPublicKey []byte

// updaterWindowCSS tints the framework update window to match Biebie Kube.
//
// The primary button's hover and press states are restated because the
// framework expresses them as filter: brightness(), which scales every channel
// and so lifts its own mid-blue accent but drains a light one: our lavender
// clips towards white and the button reads as faded rather than raised. Naming
// both shades keeps the brand tint, and matches what the application itself
// does with brand and brand-strong.
const updaterWindowCSS = `:root {
	--bg: #1c1d1f;
	--surface: #17171c;
	--surface-2: #1f1f26;
	--fg: #f4f4f5;
	--fg-dim: #a1a1aa;
	--fg-faint: #71717a;
	--border: #2a2a33;
	--accent: #cbb6e8;
	--accent-fg: #1c1d1f;
	--success: #22c55e;
	--error: #ef4444;
	--radius: 10px;
}

.u__btn--primary:hover:not(:disabled) {
	filter: none;
	background: #b89ad9;
}

.u__btn--primary:active:not(:disabled) {
	filter: none;
	background: #a98ec8;
}`

// configureUpdater wires GitHub Releases into the Wails updater.
//
// A failure here must not stop the application: clusters still open, and
// Settings can report that a check is unavailable.
func configureUpdater(app *application.App) {
	gh, err := github.New(github.Config{
		Repository:    updateRepository,
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  update.MatchAsset,
		HTTPClient:    update.NewHTTPClient(),
	})
	if err != nil {
		log.Printf("update source could not be configured: %v", err)
		return
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: appVersion,
		PublicKey:      updaterPublicKey,
		Providers:      []updater.Provider{update.WithSignatures(gh)},
		Window: &updater.BuiltinWindow{
			CSS: updaterWindowCSS,
		},
	}); err != nil {
		log.Printf("updater could not start: %v", err)
	}
}
