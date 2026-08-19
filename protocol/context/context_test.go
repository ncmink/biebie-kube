package context

import (
	"errors"
	"testing"
	"time"
)

func TestValidateRequiresIdentity(t *testing.T) {
	if err := (BiebieContext{}).Validate(); err == nil {
		t.Fatal("an empty context must not validate")
	}
}

func TestValidateRejectsCredentialShapedValues(t *testing.T) {
	c := BiebieContext{
		ContextID:   "ctx_1",
		CustomerID:  "smoi",
		ClusterID:   "rke2",
		ClusterName: "RKE2 Production token: eyJhbGciOi",
	}
	if err := c.Validate(); !errors.Is(err, ErrSecretMaterial) {
		t.Fatalf("error = %v, want ErrSecretMaterial", err)
	}
}

func TestTitleSkipsMissingParts(t *testing.T) {
	c := BiebieContext{CustomerName: "SMOI", ClusterName: "RKE2 Production"}
	if got := c.Title(); got != "SMOI / RKE2 Production" {
		t.Fatalf("title = %q", got)
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	c := BiebieContext{ExpiresAt: now.Add(time.Minute)}
	if c.Expired(now) {
		t.Fatal("must not be expired before ExpiresAt")
	}
	if !c.Expired(now.Add(2 * time.Minute)) {
		t.Fatal("must be expired after ExpiresAt")
	}
}

func TestZeroExpiryNeverExpires(t *testing.T) {
	if (BiebieContext{}).Expired(time.Now()) {
		t.Fatal("a context without an expiry must not be treated as expired")
	}
}
