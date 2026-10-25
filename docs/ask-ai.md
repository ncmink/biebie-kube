# Ask AI (local CLI)

Ask AI sends **scrubbed context for the resource open in the drawer** to an AI command-line tool installed on your machine. BiebieKube does not call a Biebie cloud service and there is no premium tier.

## How it works

1. Open a resource and choose the **Ask AI** tab (or **Ask AI about this finding** on Explain Why).
2. Pick a detected CLI (or a custom command from Settings → AI).
3. BiebieKube builds a read-only prompt (YAML, warning events, Explain Why findings, related resource names), runs the CLI headlessly with an argument vector (no shell), and streams stdout into the panel.

## What is sent

- Manifest YAML with server-managed fields removed
- No Secret `data`, `binaryData`, or `stringData`
- No `kubectl.kubernetes.io/last-applied-configuration` annotation
- Pod env vars that referenced secrets are replaced with `<secret name/key>` placeholders
- Warning events for the resource
- Explain Why summaries
- Related resource names (not full manifests)

Context is capped at about 60 KiB; larger prompts are truncated with a notice.

The **Data that will be sent** block at the top of the Ask AI panel lists each section with its size, what was removed (only items actually present on this object), and a **Show full message** toggle with the exact text. It is produced by the same Go code path as the real request, with the question left as a placeholder.

## What is not sent

- Cluster-wide inventory or other namespaces (unless they appear in related names)
- Kubeconfig, tokens, or BiebieKube process environment variables matching `KUBE*`, `KUBECONFIG`, or names ending in `TOKEN`, `SECRET`, or `PASSWORD`

The CLI runs with an empty temporary working directory.

## Supported CLIs

Built-in detection (PATH as seen by BiebieKube, including login-shell merge on macOS):

| ID | Command | Notes |
|----|---------|--------|
| `claude` | `claude` | `-p --output-format text --permission-mode plan --tools "" --no-session-persistence` |
| `codex` | `codex` | `exec --sandbox read-only` |
| `gemini` | `gemini` | Prompt on stdin |
| `cursor` | `agent` | `--print --trust --mode ask --output-format text` (empty temp cwd; binary is `agent`, not `cursor`) |

You can add **custom commands** with an absolute path and arguments. Use `{{PROMPT}}` in args to pass the full message on stdin.

## Privacy acknowledgement

The first time you use each CLI, the UI asks you to confirm that data may be sent to that tool’s provider. This choice is stored locally per CLI id.

## Limits

- Headless one-shot only (no interactive PTY in the panel)
- CLIs may still have agent tools; BiebieKube uses read-only flags where available, but you should trust the CLI you install
- Answers are rendered as safe text (no HTML execution from model output)
