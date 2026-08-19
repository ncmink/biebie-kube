// Package context defines the Biebie Context: who the customer is, which
// environment, and which cluster — the record one Biebie application hands to
// another.
//
// A context identifies things. It never contains them. No password, OTP,
// bearer token, kubeconfig body or private key may travel in this struct; only
// references that the owning application can resolve for itself.
package context

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// App is a Biebie application identifier, used to address a handoff.
type App string

// The applications that speak this protocol today.
const (
	AppAccess App = "biebie-access"
	AppKube   App = "biebie-kube"
)

// Known reports whether the application identifier is one this protocol
// version recognises.
func (a App) Known() bool {
	switch a {
	case AppAccess, AppKube:
		return true
	default:
		return false
	}
}

func (a App) String() string { return string(a) }

// Environment classifies how dangerous a mistake in this context would be.
// Biebie Kube uses it to mark production surfaces without relying on colour
// alone.
type Environment string

// Environment classifications.
const (
	EnvironmentUnknown     Environment = ""
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

// IsProduction reports whether extra confirmation is warranted.
func (e Environment) IsProduction() bool { return e == EnvironmentProduction }

// BiebieContext answers "who, where, which environment" for a piece of work.
//
// Every field is either a display string or a reference the receiving
// application resolves through its own store.
type BiebieContext struct {
	ContextID string `json:"contextId"`

	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`

	EnvironmentID   string      `json:"environmentId"`
	EnvironmentName string      `json:"environmentName"`
	EnvironmentKind Environment `json:"environmentKind,omitempty"`

	// AccessProfileID points at a Biebie Access connection profile. It is an
	// identifier, never a credential.
	AccessProfileID string `json:"accessProfileId"`

	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`

	// Server is the Kubernetes API endpoint, kept for display and for the
	// reachability check. It is not a secret.
	Server string `json:"server,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Expired reports whether the context may no longer be acted on.
func (c BiebieContext) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// Title renders the customer → environment → cluster trail shown in the UI.
func (c BiebieContext) Title() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{c.CustomerName, c.EnvironmentName, c.ClusterName} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " / ")
}

// ErrSecretMaterial is returned when a context carries something that looks
// like a credential. The transfer is refused rather than sanitised, so the
// mistake surfaces in the application that made it.
var ErrSecretMaterial = errors.New("context must not carry secret material")

// forbiddenKeys are field names that must never appear in a serialised
// context. They are checked by Sanitise on the receiving side, which sees the
// raw JSON before it becomes a struct.
var forbiddenKeys = []string{
	"password", "passwd", "secret", "otp", "mfa", "token",
	"bearer", "kubeconfig", "privatekey", "private_key", "clientkey",
	"client_key", "certificatekey", "credential",
}

// Validate checks that a context is complete enough to act on and free of
// credential material.
func (c BiebieContext) Validate() error {
	if strings.TrimSpace(c.ContextID) == "" {
		return errors.New("context id is required")
	}
	if strings.TrimSpace(c.CustomerID) == "" {
		return errors.New("customer id is required")
	}
	if strings.TrimSpace(c.ClusterID) == "" {
		return errors.New("cluster id is required")
	}
	for _, field := range []struct{ name, value string }{
		{"customerId", c.CustomerID},
		{"customerName", c.CustomerName},
		{"environmentId", c.EnvironmentID},
		{"accessProfileId", c.AccessProfileID},
		{"clusterId", c.ClusterID},
		{"clusterName", c.ClusterName},
	} {
		if looksSecret(field.value) {
			return fmt.Errorf("%w: %s", ErrSecretMaterial, field.name)
		}
	}
	return nil
}

// looksSecret is a coarse guard against a caller stuffing a credential into an
// identifier field. It cannot catch everything, and is not meant to: the real
// defence is that no field exists for a secret.
func looksSecret(value string) bool {
	lower := strings.ToLower(value)
	for _, key := range forbiddenKeys {
		if strings.Contains(lower, key+"=") || strings.Contains(lower, key+":") {
			return true
		}
	}
	return false
}

// ForbiddenKeys exposes the credential-shaped JSON keys a receiver should
// refuse, so transports can screen raw payloads before decoding.
func ForbiddenKeys() []string {
	out := make([]string, len(forbiddenKeys))
	copy(out, forbiddenKeys)
	return out
}
