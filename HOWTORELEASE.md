# Releasing Biebie Kube

A release is a git tag. Pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which builds and signs both platforms and opens a **draft** GitHub release.

The draft is the last human checkpoint. Nothing reaches an installed copy until
you publish it.

Examples below ship `v0.2.5`. Substitute your version.

## The short version

```bash
# 1. bump: build/config.yml → info.version, core.go → appVersion
wails3 task common:update:build-assets
# 2. restore CFBundleURLName in both darwin plists (see step 1)
git diff -U0 | grep -E '^[+-][^+-]' | sort -u

# 3. verify
go test ./...
(cd frontend && npx vue-tsc --noEmit)
wails3 package

# 4. ship
git add -A
git commit -m "Update version to 0.2.5 across all configurations"
git push origin master
git tag v0.2.5
git push origin v0.2.5

# 5. watch, then publish
gh run watch "$(gh run list --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId')"
gh release view v0.2.5 --json assets -q '.assets[].name'
gh release edit v0.2.5 --draft=false
```

---

## 1. Bump the version

Edit two files by hand:

| File | Field |
| --- | --- |
| `build/config.yml` | `info.version` |
| `core.go` | `appVersion` |

`appVersion` is what the updater compares against GitHub Releases. A build that
forgets it will offer itself an update forever. Release builds also stamp it
through `-X main.appVersion=…` from the tag, but the checked-in value is what
`wails3 dev` and any local package report.

Then generate the platform manifests instead of editing them:

```bash
wails3 task common:update:build-assets
```

That rewrites both darwin plists, `build/windows/info.json`, the NSIS header,
the Windows manifest and `nfpm.yaml` from `build/config.yml`.

### Restore CFBundleURLName

The generator writes its own defaults over values it does not know about. Every
time, it resets `CFBundleURLName` to `wails.com.biebie-kube` in both
`build/darwin/Info.plist` and `build/darwin/Info.dev.plist`. Put it back:

```xml
<string>net.biebie.biebie-kube</string>
```

### Read the diff before committing

Once that is restored, the diff should contain nothing but version strings:

```bash
git diff -U0 | grep -E '^[+-][^+-]' | sort -u
```

## 2. Icons, only if the artwork changed

Skip this unless you edited `build/appicon.png` or `build/appicon.icon/`.

Run the generator directly rather than through Task:

```bash
cd build && wails3 generate icons \
  -input appicon.png \
  -macfilename darwin/icons.icns \
  -windowsfilename windows/icon.ico \
  -iconcomposerinput appicon.icon \
  -macassetdir darwin
```

Then commit the three outputs, which are checked-in artefacts:

```bash
git status --short build/darwin/icons.icns build/darwin/Assets.car build/windows/icon.ico
```

`wails3 task common:generate:icons` is the same command, but its `generates:`
lists only `darwin/icons.icns` and `windows/icon.ico`. It omits
`darwin/Assets.car` — the file macOS 26 actually renders — so once the other two
are current, Task reports "up to date" and leaves the old icon in place. Adding
`darwin/Assets.car` to that list in `build/Taskfile.yml` would fix it properly.

`Assets.car` needs macOS 26 and Xcode 26. On anything older, `wails3` quietly
falls back to an `.icns` rendered from the PNG and does not say it skipped the
asset catalogue.

> **The runner cannot rebuild `Assets.car`.** Both build tasks depend on
> `common:generate:icons`, and a fresh checkout has no Task cache, so CI does run
> it — but the job runs on `macos-14`, which has no actool 26. `Assets.car`
> therefore ships exactly as committed, while `icons.icns` is regenerated from
> `appicon.png` and loses the Icon Composer treatment. macOS 26 reads
> `Assets.car` and looks right; older macOS falls back to `icons.icns` and shows
> the bare artwork with no background.

## 3. Verify locally

```bash
go test ./...
(cd frontend && npx vue-tsc --noEmit)
wails3 package        # proves the build the workflow will run
```

## 4. Commit, tag, push

The tag must sit on the commit that carries the bump. The workflow builds the
tag and nothing else.

```bash
git add -A
git commit -m "Update version to 0.2.5 across all configurations"
git push origin master

git tag v0.2.5
git push origin v0.2.5
```

Push `master` before the tag. Pushing the tag alone succeeds — git sends
whatever objects the tag needs — but leaves the released commit on no branch at
all, so the history everyone else pulls does not contain what was shipped.

## 5. Watch the build

```bash
gh run watch "$(gh run list --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId')"
```

Three jobs run: macOS (signed, notarised `.dmg` plus the updater zip), Windows
(NSIS installer plus the updater zip), then a publish job that writes
`SHA256SUMS` over every artifact and creates the draft.

`biebie-protocol` is checked out from its **default branch**, not from a tag. If
this release depends on protocol changes, push those first or the build compiles
against the wrong contract.

> **That command latches onto the newest run, not your run.** If the tag push
> never triggered anything, it silently reports the *previous* release as
> `already completed with 'success'`. See "Nothing ran" below.

## 6. Check the draft, then publish

The draft must carry nine assets: seven built plus GitHub's two source archives.

```text
biebie-kube-v0.2.5-macos-universal.dmg
biebie-kube-v0.2.5-darwin-universal.zip
biebie-kube-v0.2.5-darwin-universal.zip.sig
biebie-kube-v0.2.5-windows-amd64-installer.exe
biebie-kube-v0.2.5-windows-amd64.zip
biebie-kube-v0.2.5-windows-amd64.zip.sig
SHA256SUMS
```

The `.sig` files are what make the pinned key in `updater-key.pem` do any work.
`internal/update` fails closed, so an updater zip published without its sibling
signature is rejected by every client rather than installed unverified.

```bash
gh release view v0.2.5 --json assets -q '.assets[].name'
gh release edit v0.2.5 --draft=false
```

Publishing matters for more than tidiness:
`/repos/{owner}/{repo}/releases/latest` does not return drafts, so an
unpublished release is invisible to the updater no matter how complete it is.

## 7. Confirm the update path

From an installed copy of the previous version, Settings → check for updates
should offer the new one, verify it and swap it in.

The download has no whole-exchange timeout (see `internal/update/client.go`), so
a large asset on a slow link keeps going instead of failing at thirty seconds.

---

## When something goes wrong

### Nothing ran after the tag push

`git push` reported `* [new tag]` and no run appeared. Check that the tag really
reached the remote, then check whether Actions is up:

```bash
git ls-remote --tags origin | grep v0.2.5
curl -s https://www.githubstatus.com/api/v2/summary.json \
  | python3 -c 'import sys,json; print([c["status"] for c in json.load(sys.stdin)["components"] if c["name"]=="Actions"])'
```

An Actions incident throttles inbound events, and a push that arrives during one
is dropped without a trace — no queued run, no failed run, nothing. Deleting and
re-pushing the tag does not help, because the replacement push is throttled the
same way. Wait for the incident to clear and dispatch the same tag by hand.

### Rebuilding without moving the tag

`workflow_dispatch` takes a `version` input and an optional `protocol_ref`, so a
job that failed on infrastructure — a notarisation queue, a runner, an Actions
outage — can be re-run against the same tag:

```bash
gh workflow run release.yml -f version=v0.2.5
```

Delete the half-finished draft first, or the publish job adds a second release
for the same tag.

Two things about a dispatched run look wrong but are not. It shows up under
`master` rather than the tag, and its title is just `Release`: that is the ref
you dispatched from, and the version comes from the input. `APP_VERSION` and the
release's `tag_name` both read `inputs.version` first and only fall back to
`github.ref_name` on a tag push.

> **A dispatched run builds the dispatched ref, not the tag.** The checkout step
> for this repository pins no `ref:`, so `-f version=v0.2.5` labels the
> artifacts without changing what is compiled. It is only safe while the tag and
> `master` point at the same commit. Check before dispatching:
>
> ```bash
> git rev-parse v0.2.5 origin/master   # two identical lines
> ```

### The tag was wrong

A tag that has been pushed and built is cheaper to supersede than to move. Bump
the patch version again and ship. Only delete a tag whose draft was never
published:

```bash
gh release delete v0.2.5 --yes
git push origin :refs/tags/v0.2.5
git tag -d v0.2.5
```

---

## Secrets the workflow needs

Configured once on the repository. Listed here so a failure points at the right
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
