package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// signatureSuffix is appended to an asset's own name to locate its detached
// signature in the same release, as cmd/sign-release writes it.
const signatureSuffix = ".sig"

// maxSignatureBytes caps the sidecar read. A base64 Ed25519 signature is 88
// bytes, so anything near this limit is a redirect to an error page rather than
// a signature.
const maxSignatureBytes = 1024

// Signed wraps a release provider so downloads are authenticated as well as
// checksummed.
//
// A SHA256SUMS digest proves the bytes arrived intact, not that they came from
// our release pipeline: whoever can publish an asset can publish a matching
// digest. The Wails GitHub provider only ever populates a digest, so this
// decorator fetches the detached Ed25519 signature and attaches it, which is
// what makes the pinned public key in the application do any work.
//
// Verification fails closed. A release whose signature is missing, malformed,
// or unreadable is rejected outright rather than falling back to the digest,
// because tampering and deleting the sidecar is a single step for anyone with
// write access to the release.
type Signed struct {
	inner  updater.Provider
	client *http.Client
}

// WithSignatures returns inner decorated with detached-signature lookup.
func WithSignatures(inner updater.Provider) *Signed {
	return &Signed{
		inner:  inner,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name reports the wrapped provider's name so the Updater still routes a
// follow-up Download back to this entry in its provider list.
func (s *Signed) Name() string { return s.inner.Name() }

// Check delegates to the wrapped provider, then attaches the signature that
// authenticates the chosen asset.
func (s *Signed) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	release, err := s.inner.Check(ctx, req)
	if err != nil || release == nil {
		return release, err
	}

	assetURL, _ := release.Metadata["github.asset.url"].(string)
	if assetURL == "" {
		return nil, errors.New("update: release carries no asset URL to resolve a signature against")
	}

	signature, err := s.fetchSignature(ctx, assetURL+signatureSuffix)
	if err != nil {
		return nil, fmt.Errorf("update: %s is not signed by this project: %w", release.Artifact.Filename, err)
	}

	if release.Verification == nil {
		release.Verification = &updater.Verification{}
	}
	// The signature covers the SHA-256 digest of the asset, so the Updater has
	// to hash the download with the algorithm the signer used.
	if release.Verification.DigestAlgo == "" {
		release.Verification.DigestAlgo = "sha256"
	}
	release.Verification.SignatureAlgo = "ed25519"
	release.Verification.Signature = signature

	return release, nil
}

// Download delegates unchanged: the wrapped provider owns the transfer, and the
// Updater verifies the result against the signature Check attached.
func (s *Signed) Download(ctx context.Context, release *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	return s.inner.Download(ctx, release, dst, onProgress)
}

func (s *Signed) fetchSignature(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching signature: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching signature: HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxSignatureBytes))
	if err != nil {
		return nil, fmt.Errorf("reading signature: %w", err)
	}
	return decodeSignature(body)
}

// decodeSignature parses the base64 body of a ".sig" file into raw signature
// bytes, rejecting anything that is not exactly one Ed25519 signature.
func decodeSignature(body []byte) ([]byte, error) {
	encoded := strings.TrimSpace(string(body))
	if encoded == "" {
		return nil, errors.New("signature file is empty")
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("signature is not base64: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature is %d bytes, want %d", len(signature), ed25519.SignatureSize)
	}
	return signature, nil
}
