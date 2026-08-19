# protocol

The wire contract shared by the **biebie.net** desktop applications. These
packages live in this repo as `biebie-kube/protocol/...` so Biebie Kube
builds without a sibling checkout.

```text
Biebie Access
      ↓
   protocol/
      ↑
    Biebie Kube
```

This tree carries **no business logic**. It only describes how one Biebie
application hands a *context* — who the customer is, which environment, which
cluster — to another one, and how they talk over local IPC.

## Packages

| Package | Responsibility |
| --- | --- |
| `context` | `BiebieContext`, the who/where/which-environment record |
| `handoff` | Short-lived, single-use handoff records and their in-memory store |
| `ipc` | Loopback-free local transport: Unix socket / Windows named pipe |
| `deeplink` | `biebie-kube://` and `biebie-access://` URL parsing and building |
| `version` | Protocol version negotiation |

## Never carries secrets

A context identifies things. It never *contains* them.

Forbidden in a context, a handoff, or a deep link:

```text
VPN password · OTP · MFA token · Kubernetes bearer token
kubeconfig content · private key · client certificate key · cloud secret
```

Only references travel: `AccessProfileID`, `ClusterID`, `KubeconfigRef`,
`CredentialRef`. Sensitive material is resolved by the owning application, at
the moment it is needed, and never leaves that process.

`context.Validate` and `handoff.Store` reject a context whose fields look like
secret material, so a mistake in a calling application fails loudly instead of
leaking.

## Transport

| Platform | Transport | Location |
| --- | --- | --- |
| macOS | Unix domain socket | `~/Library/Application Support/Biebie/<app>.sock` |
| Linux | Unix domain socket | `$XDG_RUNTIME_DIR/biebie/<app>.sock` |
| Windows | Named pipe | `\\.\pipe\biebie-<app>` |

The socket directory is created `0700` and the socket itself `0600`, so another
OS user cannot consume Biebie contexts. Nothing binds to a network interface.

## Versioning

Every envelope carries `{"protocol":"biebie-context","version":1}`. A peer that
sends an unsupported major version is rejected with a clear error rather than
being half-understood, so the two applications can ship on their own schedules.
