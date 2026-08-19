package handoff

import (
	stdctx "context"
	"errors"
	"testing"
	"time"

	bctx "biebie-kube/protocol/context"
)

func sampleContext() bctx.BiebieContext {
	return bctx.BiebieContext{
		ContextID:       "ctx_01JABC",
		CustomerID:      "smoi",
		CustomerName:    "SMOI",
		EnvironmentID:   "prod",
		EnvironmentName: "Production",
		EnvironmentKind: bctx.EnvironmentProduction,
		AccessProfileID: "smoi-vpn",
		ClusterID:       "rke2-prod",
		ClusterName:     "RKE2 Production",
	}
}

func newTestStore(t *testing.T, now *time.Time, osUser string) *Store {
	t.Helper()
	return NewStore(
		WithClock(func() time.Time { return *now }),
		WithOSUser(func() (string, error) { return osUser, nil }),
	)
}

func TestHandoffRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	id, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !ValidID(id) {
		t.Fatalf("id %q is not well formed", id)
	}

	got, err := store.ConsumeFor(id, bctx.AppKube)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.ClusterID != "rke2-prod" || got.CustomerName != "SMOI" {
		t.Fatalf("context = %+v", got)
	}
}

func TestHandoffIsSingleUse(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	id, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.ConsumeFor(id, bctx.AppKube); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := store.ConsumeFor(id, bctx.AppKube); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second consume error = %v, want ErrNotFound", err)
	}
}

func TestHandoffExpires(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	id, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now = now.Add(DefaultTTL + time.Second)
	if _, err := store.ConsumeFor(id, bctx.AppKube); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrExpired) {
		t.Fatalf("expired consume error = %v", err)
	}
}

func TestHandoffRejectsWrongTargetApplication(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	id, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.ConsumeFor(id, bctx.AppAccess); !errors.Is(err, ErrWrongApp) {
		t.Fatalf("error = %v, want ErrWrongApp", err)
	}
}

func TestHandoffRejectsOtherOSUser(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	osUser := "501"
	store := NewStore(
		WithClock(func() time.Time { return now }),
		WithOSUser(func() (string, error) { return osUser, nil }),
	)

	id, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	osUser = "502"
	if _, err := store.ConsumeFor(id, bctx.AppKube); !errors.Is(err, ErrWrongUser) {
		t.Fatalf("error = %v, want ErrWrongUser", err)
	}
}

func TestHandoffRefusesSecretMaterial(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	leaky := sampleContext()
	leaky.AccessProfileID = "smoi-vpn password=secret123"

	if _, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   leaky,
	}); !errors.Is(err, bctx.ErrSecretMaterial) {
		t.Fatalf("error = %v, want ErrSecretMaterial", err)
	}
}

func TestHandoffRejectsUnreasonableLifetime(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, "501")

	if _, err := store.CreateHandoff(stdctx.Background(), ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context:   sampleContext(),
		CreatedAt: now,
		ExpiresAt: now.Add(2 * time.Hour),
	}); !errors.Is(err, ErrTTLRange) {
		t.Fatalf("error = %v, want ErrTTLRange", err)
	}
}

func TestValidIDRejectsJunk(t *testing.T) {
	for _, id := range []string{"", "hnd_", "hnd_TOOSHORT", "abc_aaaaaaaaaaaaaaaa", "hnd_AAAAAAAAAAAAAAAA"} {
		if ValidID(id) {
			t.Fatalf("%q should not be a valid handoff id", id)
		}
	}
}
