package authoring

import (
	"fmt"
	"strings"

	"biebie-kube/internal/domain"
)

// The starter an editor opens with is a starter for the kind the person was
// looking at.
//
// Opening a ConfigMap because somebody pressed Create on the Namespaces list
// is not a small annoyance: it is the screen disagreeing with itself about what
// is being made, in a dialog whose whole purpose is to be certain what is being
// made. So the kind comes from the list, and the apiVersion, the Kind spelling
// and whether it takes a namespace all come from what the cluster serves rather
// than from a table compiled in here — a custom resource gets a correct
// skeleton for the same reason a Deployment does.

// target is the object the editor is about to help write.
type target struct {
	apiVersion string
	kind       string

	// namespace is empty for a cluster-scoped kind, and the metadata field is
	// then left out entirely rather than written empty. A Namespace with a
	// `namespace:` on it is a manifest the API server rejects and a person has
	// to work out why.
	namespace  string
	namespaced bool
}

// resolve works out what creating this kind in this cluster would mean.
//
// Discovery rather than the compiled-in catalogue for the Kind spelling: the
// catalogue is keyed by plural name and a manifest is keyed by Kind, and
// singularising "endpoints" or "networkpolicies" by rule is how you end up
// generating `kind: Networkpolicie`.
func (s *Service) resolveTarget(clusterID, namespace, kind string) (target, bool) {
	info, found := s.clusters.LookupKind(clusterID, domain.Kind(kind))
	if !found {
		return target{}, false
	}

	spelling := ""
	for _, candidate := range s.clusters.APIResources(clusterID) {
		if candidate.Group == info.Group && candidate.Version == info.Version &&
			candidate.Resource == info.Resource {
			spelling = candidate.Kind
			break
		}
	}
	if spelling == "" {
		return target{}, false
	}

	out := target{apiVersion: info.Version, kind: spelling, namespaced: info.Namespaced}
	if info.Group != "" {
		out.apiVersion = info.Group + "/" + info.Version
	}
	if info.Namespaced {
		out.namespace = namespace
	}
	return out, true
}

// body is the part of a starter a skeleton cannot supply.
//
// A skeleton — apiVersion, kind, a name — is a complete and valid Namespace,
// ServiceAccount or PriorityClass, and this table is empty for all of them.
// It exists for the few kinds where a skeleton is not merely sparse but
// rejected: a Deployment without a selector and a pod template is not a
// Deployment the API server will take, and handing somebody one to fix is
// handing them a puzzle rather than a start.
//
// Both spellings are written out rather than one being derived from the other.
// A generator that turned YAML into a TypeScript literal would be a second
// thing to be wrong, for four entries.
type body struct{ yaml, typescript string }

var bodies = map[string]body{
	"ConfigMap": {
		yaml: `data:
  example: value
`,
		typescript: `  data: {
    example: 'value',
  },
`,
	},
	"Secret": {
		// stringData rather than data, because base64 typed by hand is base64
		// nobody can review. The value is a placeholder and says so.
		yaml: `type: Opaque
stringData:
  example: replace-me
`,
		typescript: `  type: 'Opaque',
  stringData: {
    example: 'replace-me',
  },
`,
	},
	"Service": {
		yaml: `spec:
  selector:
    app: example
  ports:
    - port: 80
      targetPort: 80
`,
		typescript: `  spec: {
    selector: {
      app: 'example',
    },
    ports: [
      { port: 80, targetPort: 80 },
    ],
  },
`,
	},
	"Deployment": {
		yaml: `spec:
  replicas: 1
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
        - name: example
          image: nginx:1.27
`,
		typescript: `  spec: {
    replicas: 1,
    selector: {
      matchLabels: {
        app: 'example',
      },
    },
    template: {
      metadata: {
        labels: {
          app: 'example',
        },
      },
      spec: {
        containers: [
          {
            name: 'example',
            image: 'nginx:1.27',
          },
        ],
      },
    },
  },
`,
	},
}

// yamlStarter is the YAML surface's opening text.
func yamlStarter(t target) string {
	var out strings.Builder
	fmt.Fprintf(&out, "apiVersion: %s\nkind: %s\nmetadata:\n  name: example\n", t.apiVersion, t.kind)
	if t.namespace != "" {
		fmt.Fprintf(&out, "  namespace: %s\n", t.namespace)
	}
	out.WriteString(bodies[t.kind].yaml)
	return out.String()
}

// typescriptStarter is the cdk8s surface's opening text.
//
// ApiObject rather than the generated KubeDeployment types, and that is a
// deliberate limit rather than a shortcut. The typed constructs come from
// `cdk8s import`, which downloads a schema and writes an imports/ directory per
// project; doing that behind a button would be this application installing
// packages on somebody's behalf, which is the thing the dependency policy
// exists to prevent. ApiObject is in the cdk8s package itself and takes the
// same manifest a person would have written by hand — which is the honest
// shape of this feature anyway: authoring one resource, not modelling a system.
func typescriptStarter(t target) string {
	var metadata strings.Builder
	metadata.WriteString("    name: 'example',\n")
	if t.namespace != "" {
		fmt.Fprintf(&metadata, "    namespace: '%s',\n", t.namespace)
	}

	return fmt.Sprintf(`import { App, ApiObject, Chart } from 'cdk8s';

const app = new App();
const chart = new Chart(app, 'resource');

new ApiObject(chart, '%s', {
  apiVersion: '%s',
  kind: '%s',
  metadata: {
%s  },
%s});

app.synth();
`, strings.ToLower(t.kind), t.apiVersion, t.kind, metadata.String(), bodies[t.kind].typescript)
}
