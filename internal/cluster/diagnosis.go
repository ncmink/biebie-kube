package cluster

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"biebie-kube/internal/domain"
)

// tcpProbeTimeout bounds the port check. Behind a VPN that is down, a TCP
// connection to a private address hangs for a long time; the point of probing
// is to tell the user which layer failed quickly, not to wait out the stack.
const tcpProbeTimeout = 4 * time.Second

// classify turns a client-go or network error into the failure the user should
// act on.
//
// The categories exist because the fixes are completely different: an expired
// certificate is the engineer's problem, a forbidden verb is the cluster
// owner's, and a TCP timeout is usually the VPN.
func classify(err error) (domain.FailureKind, string) {
	if err == nil {
		return domain.FailureNone, ""
	}

	// The exec plugin is checked before anything else. client-go wraps a
	// missing helper in the same request error as a network failure, so left
	// to the cases below it reads as an unreachable cluster and sends the
	// engineer to look at their VPN.
	if kind, summary := classifyExec(err.Error()); kind != domain.FailureNone {
		return kind, summary
	}

	switch {
	case apierrors.IsUnauthorized(err):
		return domain.FailureUnauthorized, "The cluster rejected these credentials."
	case apierrors.IsForbidden(err):
		return domain.FailureForbidden, "These credentials are not allowed to perform this operation."
	case apierrors.IsServiceUnavailable(err), apierrors.IsInternalError(err):
		return domain.FailureAPIUnavailable, "The Kubernetes API server is not serving requests."
	}

	var certErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var expiredErr x509.CertificateInvalidError
	if errors.As(err, &certErr) || errors.As(err, &hostErr) || errors.As(err, &expiredErr) {
		return domain.FailureTLS, "The cluster's TLS certificate was not accepted."
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return domain.FailureDNS, fmt.Sprintf("The name %s could not be resolved.", dnsErr.Name)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return domain.FailureTCPTimeout, "The connection to the API server timed out."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.FailureTCPTimeout, "The connection to the API server timed out."
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "no route to host"),
		strings.Contains(message, "network is unreachable"):
		return domain.FailureNoRoute, "There is no route to the API server from this machine."
	case strings.Contains(message, "connection refused"):
		return domain.FailureAPIUnavailable, "The API server refused the connection."
	case strings.Contains(message, "certificate"), strings.Contains(message, "x509"):
		return domain.FailureTLS, "The cluster's TLS certificate was not accepted."
	case strings.Contains(message, "Unauthorized"):
		return domain.FailureUnauthorized, "The cluster rejected these credentials."
	}

	return domain.FailureAPIUnavailable, "The cluster could not be reached."
}

// execPrefix is how client-go introduces both of its exec plugin failures.
// The typed *exec.Error is discarded before the error reaches a caller, so the
// message is the only thing left to read.
const execPrefix = "exec: executable "

// classifyExec recognises a kubeconfig exec plugin that could not produce a
// token, and names the binary at fault.
//
// The name matters more than the category here: "aws" and "gke-gcloud-auth-
// plugin" are installed in completely different ways, and a message that omits
// which one is missing leaves the user to read the raw error anyway.
func classifyExec(message string) (domain.FailureKind, string) {
	start := strings.Index(message, execPrefix)
	if start < 0 {
		return domain.FailureNone, ""
	}
	rest := message[start+len(execPrefix):]

	if name, ok := commandBefore(rest, " not found"); ok {
		return domain.FailureCredentialHelper, fmt.Sprintf(
			"The credential helper %q is not installed, or is not on the PATH this application was started with.", name)
	}
	if name, ok := commandBefore(rest, " failed with exit code"); ok {
		return domain.FailureCredentialHelper, fmt.Sprintf(
			"The credential helper %q ran but did not return a token.", name)
	}
	return domain.FailureNone, ""
}

// commandBefore reads a command name that ends at sep.
//
// Rejecting a name containing a space keeps the two message forms apart: the
// exit code message also ends with the word "found" further along, and would
// otherwise be read as a missing binary whose name is the rest of the
// sentence.
func commandBefore(rest, sep string) (string, bool) {
	name, _, found := strings.Cut(rest, sep)
	if !found || name == "" || strings.ContainsAny(name, " \t\n") {
		return "", false
	}
	return name, true
}

// stateFor maps a failure to the state the session lands in, so the UI shows
// one consistent status rather than each caller inventing its own.
func stateFor(kind domain.FailureKind) domain.ClusterState {
	switch kind {
	case domain.FailureNone:
		return domain.ClusterConnected
	case domain.FailureAccessDown:
		return domain.ClusterWaitingAccess
	case domain.FailureUnauthorized, domain.FailureForbidden:
		return domain.ClusterUnauthorized
	case domain.FailureConfig, domain.FailureCredentialHelper:
		return domain.ClusterFailed
	default:
		return domain.ClusterUnreachable
	}
}

// probeTCP reports whether the API server's port accepts a connection.
//
// This separates "the network cannot reach the host" from "Kubernetes said
// no", which is the distinction that decides whether the engineer should look
// at Biebie Access or at their kubeconfig.
func probeTCP(ctx context.Context, hostPort string) (time.Duration, error) {
	if hostPort == "" {
		return 0, errors.New("the cluster has no API endpoint recorded")
	}
	started := time.Now()
	dialer := net.Dialer{Timeout: tcpProbeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", hostPort)
	elapsed := time.Since(started)
	if err != nil {
		return elapsed, err
	}
	_ = conn.Close()
	return elapsed, nil
}

// builder accumulates probe lines in the order they were attempted.
type builder struct {
	probes []domain.Probe
}

func (b *builder) pass(layer domain.Layer, detail string, elapsed time.Duration) {
	b.probes = append(b.probes, domain.Probe{
		Layer:   layer,
		Result:  domain.ProbePassed,
		Detail:  detail,
		Elapsed: elapsed.Milliseconds(),
	})
}

func (b *builder) fail(layer domain.Layer, detail string, elapsed time.Duration) {
	b.probes = append(b.probes, domain.Probe{
		Layer:   layer,
		Result:  domain.ProbeFailed,
		Detail:  detail,
		Elapsed: elapsed.Milliseconds(),
	})
}

// skip marks one layer as untested and says why, for a step that was passed
// over rather than attempted — a question Biebie Kube could not ask reads as
// skipped, not as an answer of "no".
func (b *builder) skip(layer domain.Layer, detail string) {
	b.probes = append(b.probes, domain.Probe{
		Layer:  layer,
		Result: domain.ProbeSkipped,
		Detail: detail,
	})
}

// skipRest marks the layers that were never tested.
//
// Reporting an untested layer as failed would suggest several problems where
// there is one, and send the engineer looking in the wrong place.
func (b *builder) skipRest(layers ...domain.Layer) {
	for _, layer := range layers {
		b.probes = append(b.probes, domain.Probe{Layer: layer, Result: domain.ProbeSkipped})
	}
}

func (b *builder) diagnosis(kind domain.FailureKind, summary, detail, profileID string) *domain.Diagnosis {
	return &domain.Diagnosis{
		Kind:            kind,
		Summary:         summary,
		Detail:          detail,
		Probes:          b.probes,
		AccessProfileID: profileID,
	}
}
