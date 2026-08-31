package gitops

import (
	"testing"

	"biebie-kube/internal/domain"
)

// The manifest a person actually writes: everything they decided, and nothing
// they did not.
const sourceDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ak-super-auto
spec:
  selector:
    matchLabels: {app: super-auto-develop}
  template:
    metadata:
      labels: {app: super-auto-develop}
    spec:
      containers:
        - name: super-auto-develop
          image: super-auto-develop
      volumes:
        - name: tz-bangkok
          hostPath:
            path: /usr/share/zoneinfo/Asia/Bangkok
`

// The same Deployment as the API server returns it, after defaulting, after
// the controller wrote its revision and after Argo CD labelled it. This is the
// object that produced ten differences of which one mattered.
const liveDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ak-super-auto
  annotations:
    deployment.kubernetes.io/revision: "22"
  labels:
    argocd.argoproj.io/instance: ak-super-auto
spec:
  replicas: 1
  progressDeadlineSeconds: 600
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
    rollingUpdate: {maxSurge: 25%, maxUnavailable: 25%}
  selector:
    matchLabels: {app: super-auto-develop}
  template:
    metadata:
      creationTimestamp: null
      labels: {app: super-auto-develop}
    spec:
      restartPolicy: Always
      dnsPolicy: ClusterFirst
      schedulerName: default-scheduler
      terminationGracePeriodSeconds: 30
      securityContext: {}
      containers:
        - name: super-auto-develop
          image: 708607833758.dkr.ecr.ap-southeast-1.amazonaws.com/super-auto-develop:6520
          imagePullPolicy: IfNotPresent
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
          resources: {}
      volumes:
        - name: tz-bangkok
          hostPath:
            path: /usr/share/zoneinfo/Asia/Bangkok
            type: ""
`

func TestTheDeploymentThatReportedTenDifferencesReportsOne(t *testing.T) {
	// The whole point of this slice, as one test. Nine of the ten rows were
	// Kubernetes describing itself; one was an image nobody could account for,
	// and it was in the middle of the list looking exactly like the rest.
	found := classify(differencesBetween(t, sourceDeployment, object(t, liveDeployment), false))

	var meaningful []domain.StateDifference
	for _, difference := range found {
		if difference.Class == domain.DifferenceMeaningful {
			meaningful = append(meaningful, difference)
		}
	}

	if len(meaningful) != 1 {
		t.Fatalf("meaningful = %d, want 1: %+v", len(meaningful), meaningful)
	}
	if meaningful[0].Path != "spec.template.spec.containers[name=super-auto-develop].image" {
		t.Fatalf("the wrong difference survived: %+v", meaningful[0])
	}
	if meaningful[0].Label != "Container image" || meaningful[0].Subject != "super-auto-develop" {
		t.Fatalf("label = %q, subject = %q", meaningful[0].Label, meaningful[0].Subject)
	}

	// Seven of the ten were normalised out of existence rather than hidden,
	// because a Deployment with no replicas in Git and one replica running is
	// not two states. Two genuinely exist and are set aside with a reason.
	if len(found) != 3 {
		t.Fatalf("differences = %d, want 3 (one meaningful, two system-managed): %+v", len(found), found)
	}
}

func TestAnOmittedFieldEqualToItsDefaultIsNotADifference(t *testing.T) {
	// Each of these was a row in the report. None of them is a disagreement:
	// the source is silent and the cluster holds the value Kubernetes puts
	// there when a manifest is silent.
	for name, live := range map[string]string{
		"replicas":                      `spec: {replicas: 1}`,
		"progressDeadlineSeconds":       `spec: {progressDeadlineSeconds: 600}`,
		"revisionHistoryLimit":          `spec: {revisionHistoryLimit: 10}`,
		"rolling update strategy":       `spec: {strategy: {type: RollingUpdate, rollingUpdate: {maxSurge: 25%, maxUnavailable: 25%}}}`,
		"schedulerName":                 `spec: {template: {spec: {schedulerName: default-scheduler}}}`,
		"empty pod securityContext":     `spec: {template: {spec: {securityContext: {}}}}`,
		"restartPolicy":                 `spec: {template: {spec: {restartPolicy: Always}}}`,
		"dnsPolicy":                     `spec: {template: {spec: {dnsPolicy: ClusterFirst}}}`,
		"terminationGracePeriodSeconds": `spec: {template: {spec: {terminationGracePeriodSeconds: 30}}}`,
		"serviceAccountName":            `spec: {template: {spec: {serviceAccountName: default}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"
			document := source + live + "\n"
			found := differencesBetween(t, source, object(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"+live+"\n"), false)
			if len(found) != 0 {
				t.Fatalf("reported %+v for %s", found, document)
			}
		})
	}
}

func TestAnOmittedHostPathTypeIsNotADifference(t *testing.T) {
	// The API server serialises "no checks" as the empty string rather than by
	// leaving the field out. This is decided for this one field rather than by
	// treating missing and empty as equal everywhere, which is not true.
	source := `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec:
  template:
    spec:
      volumes:
        - name: tz
          hostPath: {path: /usr/share/zoneinfo/Asia/Bangkok}
`
	live := object(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec:
  template:
    spec:
      volumes:
        - name: tz
          hostPath: {path: /usr/share/zoneinfo/Asia/Bangkok, type: ""}
`)
	if found := differencesBetween(t, source, live, false); len(found) != 0 {
		t.Fatalf("reported %+v", found)
	}
}

func TestAnEmptyStringIsNotTreatedAsMissingEverywhere(t *testing.T) {
	// The guard on the rule above. An empty string somewhere Kubernetes does
	// not document one is a value somebody set, and hiding it would be the
	// generic `missing == empty` rule the field-by-field table exists to avoid.
	header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"
	source := header + "spec: {template: {spec: {hostname: api}}}\n"
	live := object(t, header+"spec: {template: {spec: {hostname: api, subdomain: \"\"}}}\n")

	found := differencesBetween(t, source, live, false)
	if len(found) != 1 || found[0].Path != "spec.template.spec.subdomain" {
		t.Fatalf("reported %+v", found)
	}
}

func TestAValueThatIsNotTheDefaultStillDiffers(t *testing.T) {
	// The other half of every rule in the table. A default is removed because
	// the two states are the same, not because the field is uninteresting.
	for name, test := range map[string]struct {
		source, live, path string
	}{
		"replicas": {
			source: "spec: {replicas: 2}",
			live:   "spec: {replicas: 1}",
			path:   "spec.replicas",
		},
		"strategy": {
			source: "spec: {strategy: {type: Recreate}}",
			live:   "spec: {strategy: {type: RollingUpdate}}",
			path:   "spec.strategy.type",
		},
		"scheduler": {
			source: "spec: {template: {spec: {schedulerName: default-scheduler}}}",
			live:   "spec: {template: {spec: {schedulerName: custom-scheduler}}}",
			path:   "spec.template.spec.schedulerName",
		},
		"grace period": {
			source: "spec: {template: {spec: {terminationGracePeriodSeconds: 60}}}",
			live:   "spec: {template: {spec: {terminationGracePeriodSeconds: 30}}}",
			path:   "spec.template.spec.terminationGracePeriodSeconds",
		},
	} {
		t.Run(name, func(t *testing.T) {
			header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"
			found := classify(differencesBetween(t, header+test.source+"\n", object(t, header+test.live+"\n"), false))

			if len(found) != 1 {
				t.Fatalf("reported %+v", found)
			}
			if found[0].Path != test.path {
				t.Fatalf("path = %q, want %q", found[0].Path, test.path)
			}
			if found[0].Class != domain.DifferenceMeaningful {
				t.Fatalf("a value somebody chose was set aside as %q", found[0].Class)
			}
		})
	}
}

func TestDefaultsAreNotAppliedToKindsNobodyHasReadTheReferenceFor(t *testing.T) {
	// `replicas: 1` is the Deployment default and also a value a CRD could
	// mean anything by. The table is keyed by kind so that adding a kind is a
	// deliberate act rather than something that happens by accident.
	header := "apiVersion: example.com/v1\nkind: Widget\nmetadata: {name: payment-api}\n"
	source := header + "spec: {size: large}\n"
	live := object(t, header+"spec: {size: large, replicas: 1}\n")

	found := differencesBetween(t, source, live, false)
	if len(found) != 1 || found[0].Path != "spec.replicas" {
		t.Fatalf("reported %+v", found)
	}
}

func TestAPullPolicyIsDefaultedFromTheImageTag(t *testing.T) {
	// The one rule that depends on another field: an untagged image or one
	// tagged `latest` is pulled every time, anything else only when missing.
	for name, test := range map[string]struct {
		image, policy string
		differs       bool
	}{
		"tagged image, IfNotPresent":  {image: "api:v1.8", policy: "IfNotPresent"},
		"untagged image, Always":      {image: "api", policy: "Always"},
		"latest image, Always":        {image: "api:latest", policy: "Always"},
		"registry port is not a tag":  {image: "registry:5000/api", policy: "Always"},
		"tagged image set to Always":  {image: "api:v1.8", policy: "Always", differs: true},
		"latest image set to IfNotPr": {image: "api:latest", policy: "IfNotPresent", differs: true},
	} {
		t.Run(name, func(t *testing.T) {
			header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"
			source := header + "spec: {template: {spec: {containers: [{name: api, image: \"" + test.image + "\"}]}}}\n"
			live := object(t, header+"spec: {template: {spec: {containers: [{name: api, image: \""+
				test.image+"\", imagePullPolicy: "+test.policy+"}]}}}\n")

			found := differencesBetween(t, source, live, false)
			if test.differs && len(found) != 1 {
				t.Fatalf("a pull policy that is not the default was removed: %+v", found)
			}
			if !test.differs && len(found) != 0 {
				t.Fatalf("reported %+v", found)
			}
		})
	}
}

func TestASidecarTheSourceNeverMentionsIsOneDifference(t *testing.T) {
	// An injected container arrives with every one of its defaults filled in.
	// Without pairing list elements before defaulting, it would report as one
	// container plus a handful of fields nobody wrote.
	source := `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
`
	live := object(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
        - name: istio-proxy
          image: "istio:1.20"
          imagePullPolicy: IfNotPresent
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
          resources: {}
`)
	found := differencesBetween(t, source, live, false)
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.template.spec.containers[name=istio-proxy]" {
		t.Fatalf("path = %q", found[0].Path)
	}
}
