package argocd

import "biebie-kube/internal/domain"

// This file is the one place a mutation gate is decided.
//
// There are two ways to arrive at the question — an object that exists and one
// that does not — and they gather different evidence. What they must not have
// is different rules, because "Create is blocked and Edit is not" for the same
// target is the shape of every gap this file exists to close.
//
// The rule, stated once:
//
//	Managed    a claim was found            direct persistent write refused
//	Unmanaged  the check ran and found none direct persistent write offered
//	Unknown    the check did not settle     direct persistent write refused
//
// There is no fourth branch and no override.

// GateForClaim answers for a target that does not exist yet.
func GateForClaim(claim Claim) domain.MutationGate {
	out := domain.MutationGate{
		Claim:       claim.Kind,
		Uncertainty: claim.Uncertainty,
		Reason:      claim.Reason,
		App:         claim.App,
		Probes:      claim.Probes,
		Managed:     claim.Kind.Managed(),
	}

	switch {
	case claim.Kind.Managed():
		out.Status = domain.OwnershipStatusManaged
	case claim.Kind == domain.ClaimUnknown:
		out.Status = domain.OwnershipStatusUnknown
	default:
		out.Status = domain.OwnershipStatusUnmanaged
		out.Allowed = true
	}
	return out
}

// GateForOwnership answers for an object that already exists.
//
// The evidence here is about the object itself — Argo CD's tracking
// annotation, or the Application's own list of what it manages — so a
// namespace claim never appears. That is deliberate: an Application deploying
// into a namespace is a reason to be careful about creating a new name in it,
// and is not a reason to refuse an edit to an object that Application has
// never mentioned. Treating the two the same would lock the YAML editor for
// every object in any namespace Argo CD touches.
func GateForOwnership(found domain.ResourceOwnership) domain.MutationGate {
	out := domain.MutationGate{
		Status:      found.Status,
		Uncertainty: found.Uncertainty,
		Reason:      found.Reason,
		App:         found.App,
		Probes:      found.Probes,
	}

	switch found.Status {
	case domain.OwnershipStatusManaged:
		out.Managed = true
		out.Claim = domain.ClaimObject
		// The evidence sentence says how ownership was established, which is
		// not the same as saying what to do instead. A banner over a
		// read-only editor has to answer the second question, so the
		// consequence is spelled out here rather than left to a screen to
		// word for itself.
		out.Reason = found.Reason + " Persistent changes belong in its Git source: an edit made here is" +
			" reverted by the next reconcile and recorded nowhere."
		if app := found.App; app != nil {
			out.Reason = "Argo CD Application " + app.Name + " manages this object. " +
				"Persistent changes belong in its Git source: an edit made here is reverted by the" +
				" next reconcile and recorded nowhere."
		}
	case domain.OwnershipStatusUnmanaged:
		out.Allowed = true
		out.Claim = domain.ClaimNone
	default:
		out.Status = domain.OwnershipStatusUnknown
		out.Claim = domain.ClaimUnknown
	}
	return out
}

// UnknownGate is the answer when the check could not be attempted at all.
//
// It exists so that a caller with no Argo CD service to ask cannot fall through
// to a zero-valued gate, whose Status would be the empty string and whose
// Allowed would be false for no stated reason.
func UnknownGate(reason string) domain.MutationGate {
	return domain.MutationGate{
		Status:      domain.OwnershipStatusUnknown,
		Claim:       domain.ClaimUnknown,
		Uncertainty: domain.UncertaintyUnreachable,
		Reason:      reason,
	}
}
