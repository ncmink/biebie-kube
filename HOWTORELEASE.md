# Releasing Biebie Kube

A release is a git tag. Pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which builds and signs both platforms and opens a **draft** GitHub release. The
draft is the last human checkpoint: nothing reaches an installed application
until it is published.

Worked example below is `v0.2.3`; substitute the version you are shipping.

---

## 1. Bump the version

Two files are edited by hand:

- `build/config.yml` → `info.version`
- `core.go` → `appVersion`

`appVersion` is what the updater compares against GitHub Releases, so a build
that forgets it will offer itself an update forever. Release builds also stamp
it through `-X main.appVersion=…` from the tag, but the checked-in value is what
`wails3 dev` and any local package report.

Then regenerate the platform manifests rather than editing them:

```bash
wails3 task common:update:build-assets
```

That rewrites the plists, `build/windows/info.json`, the NSIS header, the
Windows manifest and `nfpm.yaml` from `build/config.yml`.

**Check the regeneration diff before committing.** The generator writes its own
defaults over values it does not know about: it resets `CFBundleURLName` in both
`build/darwin/Info.plist` and `build/darwin/Info.dev.plist` to
`wails.com.biebie-kube`, and that has to go back to `net.biebie.biebie-kube`.
The diff should contain nothing but version strings once that is restored:

```bash
git diff -U0 | rg '^[+-][^+-]' | sort -u
```

## 2. Verify locally

```bash
go test ./...
cd frontend && npx vue-tsc --noEmit && cd ..
wails3 package                 # proves the build the workflow will run
```

## 3. Commit, tag, push

The tag must sit on the commit that carries the bump, because the workflow
builds the tag and nothing else.

```bash
git add -A
git commit -m "Update version to 0.2.3 across all configurations"
git push origin master

git tag v0.2.3
git push origin v0.2.3
```

## 4. Watch the build

```bash
gh run watch "$(gh run list --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId')"
```

Three jobs: macOS (signed, notarised `.dmg` plus the updater zip), Windows (NSIS
installer plus the updater zip), then a publish job that writes `SHA256SUMS`
over every artifact and creates the draft.

`biebie-protocol` is checked out from its **default branch**, not from a tag. If
this release depends on protocol changes, push those first or the build compiles
against the wrong contract.

## 5. Check the draft, then publish

The draft must carry nine assets — seven built plus GitHub's two source
archives:

```text
biebie-kube-v0.2.3-macos-universal.dmg
biebie-kube-v0.2.3-darwin-universal.zip
biebie-kube-v0.2.3-darwin-universal.zip.sig
biebie-kube-v0.2.3-windows-amd64-installer.exe
biebie-kube-v0.2.3-windows-amd64.zip
biebie-kube-v0.2.3-windows-amd64.zip.sig
SHA256SUMS
```

The `.sig` files are what make the pinned key in `updater-key.pem` do any work:
`internal/update` fails closed, so an updater zip published without its sibling
signature is rejected by every client rather than installed unverified.

```bash
gh release view v0.2.3 --json assets -q '.assets[].name'
gh release edit v0.2.3 --draft=false
```

Publishing matters for more than tidiness: `/repos/{owner}/{repo}/releases/latest`
does not return drafts, so an unpublished release is invisible to the updater no
matter how complete it is.

## 6. Confirm the update path

From an installed copy of the previous version, Settings → check for updates
should offer the new one, verify it and swap it in. The download has no
whole-exchange timeout (see `internal/update/client.go`), so a large asset on a
slow link keeps going instead of failing at thirty seconds.

---

## Rebuilding without moving the tag

`workflow_dispatch` takes a `version` input and an optional `protocol_ref`, so a
job that failed on infrastructure — a notarisation queue, a runner — can be
re-run against the same tag:

```bash
gh workflow run release.yml -f version=v0.2.3
```

Delete the half-finished draft first, or the publish job adds a second release
for the same tag.

## If the tag was wrong

A tag that has been pushed and built is cheaper to supersede than to move: bump
the patch version again and ship. Only delete a tag whose draft was never
published.

```bash
gh release delete v0.2.3 --yes
git push origin :refs/tags/v0.2.3
git tag -d v0.2.3
```

## Secrets the workflow needs

Configured once on the repository; listed here so a failure points at the right
one.

| Secret | Used for |
| --- | --- |
| `APPLE_DEVELOPER_CERTIFICATE_P12_BASE64`, `APPLE_DEVELOPER_CERTIFICATE_PASSWORD` | Developer ID signing of the `.app` and `.dmg` |
| `APPLE_NOTARIZATION_KEY_P8_BASE64`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID` | Notarising and stapling the `.dmg` |
| `UPDATER_SIGNING_KEY` | Ed25519 private half of `updater-key.pem`, signs the updater zips |
| `PROTOCOL_TOKEN` | Checking out the private `biebie-protocol` |

`UPDATER_SIGNING_KEY` must stay the pair of the embedded `updater-key.pem`.
Rotating one without the other ships a release that no installed copy will
accept, and those copies can only be updated by hand.
