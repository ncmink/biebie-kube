# Node capacity and headroom

Biebie Kube shows per-node capacity on the cluster Overview and in the node
inspector. The numbers match what you would read from:

```bash
kubectl top nodes
kubectl describe node <name>   # Allocated resources section
kubectl get pods -A --field-selector spec.nodeName=<name>
```

## What each number means

| UI field | kubectl equivalent | Meaning |
| --- | --- | --- |
| CPU / Memory used | `kubectl top nodes` | Actual usage from metrics-server |
| CPU / Memory allocatable | `status.allocatable` | What the scheduler may place on the node |
| CPU / Memory req % | `describe` → Requests | Sum of pod **requests** already reserved |
| Pods used / max | pod count vs `allocatable.pods` | Pod slots consumed vs the node's ceiling |
| Waiting for a node | Pending pods with no `nodeName` | Work queued but not yet assigned |

## Requests, not limits

The scheduler admits pods using **requests**, not limits. A cluster may show
limits far above 100% of allocatable — that is normal overcommit. The req %
columns are the real headroom for scheduling.

## Which pods are counted

A pod on a node counts toward that node's totals only when:

- it has `spec.nodeName` set, and
- its phase is not `Succeeded` or `Failed`.

Completed jobs therefore do not reduce free pod slots the way `kubectl get pods
| wc -l` would suggest.

Pod requests follow the scheduler rule:

```text
max(sum(container requests), max(initContainer requests)) + overhead
```

## When data is missing

- No metrics-server: usage columns stay empty; requests and pod counts still
  render.
- RBAC cannot list nodes: the Node capacity section is omitted entirely.
- RBAC cannot list pods cluster-wide: per-node requests and pod counts are
  omitted from the inspector.
