package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/incident"
	"biebie-kube/internal/resources"
)

const maxContextBytes = 60 * 1024

const readOnlyPreamble = `You are helping a Kubernetes operator understand one resource.
Answer only from the context below. Do not run commands, change files, or access networks.
If the context is insufficient, say what is missing.

`

const questionPlaceholder = "(your question)"

// Builder assembles scrubbed resource context for Ask AI.
type Builder struct {
	Resources *resources.Service
	Incident  *incident.Service
}

// Prompt is the message for the CLI and the account of what went into it.
type Prompt struct {
	Body    string
	Preview domain.AIContextPreview
}

// Build returns the full stdin message for the CLI with its summary.
//
// The preview and the message come from one call so the summary a person
// reads cannot drift from what is actually sent.
func (b *Builder) Build(ctx context.Context, clusterID string, ref domain.ResourceRef, userPrompt string) (Prompt, error) {
	obj, err := b.Resources.Get(ctx, clusterID, ref)
	if err != nil {
		return Prompt{}, err
	}
	if ref.Kind == domain.KindSecret || strings.EqualFold(obj.GetKind(), "Secret") {
		if err := ensureSecretScrubbed(obj); err != nil {
			return Prompt{}, err
		}
	}

	yamlText, scrubbed, err := scrubYAML(obj)
	if err != nil {
		return Prompt{}, err
	}

	question := strings.TrimSpace(userPrompt)
	if question == "" {
		question = questionPlaceholder
	}

	var bodies []string
	var sections []domain.AIContextSection
	add := func(title, detail, body string) {
		bodies = append(bodies, body)
		sections = append(sections, domain.AIContextSection{
			Title: title, Detail: detail, Bytes: len(body), Included: true,
		})
	}
	skip := func(title, detail string) {
		sections = append(sections, domain.AIContextSection{Title: title, Detail: detail})
	}

	add("Your question", "", "## User question\n"+question)
	add("Resource identity", fmt.Sprintf("%s %s", ref.Kind, qualifiedName(ref)),
		fmt.Sprintf("## Resource\nkind: %s\nnamespace: %s\nname: %s", ref.Kind, ref.Namespace, ref.Name))
	add("Manifest YAML", "Scrubbed copy of the live object",
		"## Manifest (scrubbed)\n```yaml\n"+yamlText+"\n```")

	if lines, err := b.warningEvents(ctx, clusterID, ref); err != nil {
		skip("Warning events", "Could not be read: "+err.Error())
	} else if len(lines) == 0 {
		skip("Warning events", "None")
	} else {
		add("Warning events", plural(len(lines), "event"), "## Warning events\n"+strings.Join(lines, "\n"))
	}

	if b.Incident != nil {
		if report, err := b.Incident.Explain(ctx, clusterID, ref); err != nil {
			skip("Explain Why findings", "Could not be collected: "+err.Error())
		} else {
			add("Explain Why findings", plural(len(report.Findings), "finding"), incidentSection(report))
		}
	}

	if groups, err := b.Resources.Related(ctx, clusterID, ref); err != nil || len(groups) == 0 {
		skip("Related resources", "None")
	} else {
		items := 0
		for _, g := range groups {
			items += len(g.Rows)
		}
		add("Related resources", fmt.Sprintf("%s in %s (names only)", plural(items, "name"), plural(len(groups), "group")),
			relatedSection(groups))
	}

	body := readOnlyPreamble + strings.Join(bodies, "\n\n")
	truncated := len(body) > maxContextBytes
	if truncated {
		body = body[:maxContextBytes] + "\n\n[Context truncated at 60 KiB]"
	}

	return Prompt{
		Body: body,
		Preview: domain.AIContextPreview{
			Resource:   ref,
			Sections:   sections,
			Scrubbed:   scrubbed,
			TotalBytes: len(body),
			Truncated:  truncated,
			Prompt:     body,
		},
	}, nil
}

func qualifiedName(ref domain.ResourceRef) string {
	if ref.Namespace == "" {
		return ref.Name
	}
	return ref.Namespace + "/" + ref.Name
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (b *Builder) warningEvents(ctx context.Context, clusterID string, ref domain.ResourceRef) ([]string, error) {
	var rows []domain.EventRow
	var err error
	if ref.Kind == domain.KindNode {
		rows, err = b.Resources.NodeEvents(ctx, clusterID, ref.Name)
	} else {
		rows, err = b.Resources.Events(ctx, clusterID, ref.Namespace, ref.Name)
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, ev := range rows {
		if !strings.EqualFold(ev.Type, "Warning") {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", ev.Reason, ev.Message))
	}
	return lines, nil
}

func incidentSection(report domain.IncidentReport) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Coverage: %s", report.Coverage.State))
	for _, f := range report.Findings {
		parts = append(parts, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Summary, f.Explanation))
		if len(f.NextSteps) > 0 {
			parts = append(parts, "  Next: "+strings.Join(f.NextSteps, "; "))
		}
	}
	return "## Explain Why findings\n" + strings.Join(parts, "\n")
}

func relatedSection(groups []domain.RelatedGroup) string {
	var lines []string
	for _, g := range groups {
		names := make([]string, 0, len(g.Rows))
		for _, row := range g.Rows {
			if g.Namespaced && row.Namespace != "" {
				names = append(names, row.Namespace+"/"+row.Name)
			} else {
				names = append(names, row.Name)
			}
		}
		title := g.Title
		if title == "" {
			title = string(g.Kind)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", title, strings.Join(names, ", ")))
	}
	return "## Related resources\n" + strings.Join(lines, "\n")
}

// scrubYAML renders a cleaned copy and reports what was removed from it.
func scrubYAML(obj *unstructured.Unstructured) (string, []string, error) {
	clean := obj.DeepCopy()
	notes := scrubObject(clean.Object)
	encoded, err := yaml.Marshal(clean.Object)
	if err != nil {
		return "", nil, fmt.Errorf("scrub YAML: %w", err)
	}
	return string(encoded), notes, nil
}

func scrubObject(obj map[string]interface{}) []string {
	var notes []string
	remove := func(note string, fields ...string) {
		if _, found, _ := unstructured.NestedFieldNoCopy(obj, fields...); found {
			unstructured.RemoveNestedField(obj, fields...)
			notes = append(notes, note)
		}
	}

	remove("metadata.managedFields", "metadata", "managedFields")
	remove("last-applied-configuration annotation",
		"metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")

	kind, _, _ := unstructured.NestedString(obj, "kind")
	if strings.EqualFold(kind, "Secret") {
		remove("Secret data", "data")
		remove("Secret binaryData", "binaryData")
		remove("Secret stringData", "stringData")
	}

	if n := scrubPodSpec(obj); n > 0 {
		notes = append(notes, fmt.Sprintf("%s from secretKeyRef replaced with placeholders", plural(n, "env var")))
	}
	return notes
}

// podSpecPaths are where a pod spec lives on the kinds Ask AI is used with.
var podSpecPaths = [][]string{
	{"spec"},
	{"spec", "template", "spec"},
	{"spec", "jobTemplate", "spec", "template", "spec"},
}

// scrubPodSpec replaces secret-backed env vars with a named placeholder and
// returns how many it replaced.
func scrubPodSpec(obj map[string]interface{}) int {
	replaced := 0
	for _, base := range podSpecPaths {
		for _, list := range []string{"containers", "initContainers"} {
			path := append(append([]string{}, base...), list)
			containers, found, _ := unstructured.NestedSlice(obj, path...)
			if !found {
				continue
			}
			for i, c := range containers {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				env, ok, _ := unstructured.NestedSlice(cm, "env")
				if !ok {
					continue
				}
				for j, e := range env {
					em, ok := e.(map[string]interface{})
					if !ok {
						continue
					}
					ref, ok, _ := unstructured.NestedMap(em, "valueFrom", "secretKeyRef")
					if !ok {
						continue
					}
					name, _, _ := unstructured.NestedString(ref, "name")
					key, _, _ := unstructured.NestedString(ref, "key")
					em["value"] = fmt.Sprintf("<secret %s/%s>", name, key)
					delete(em, "valueFrom")
					env[j] = em
					replaced++
				}
				cm["env"] = env
				containers[i] = cm
			}
			_ = unstructured.SetNestedSlice(obj, containers, path...)
		}
	}
	return replaced
}

func ensureSecretScrubbed(obj *unstructured.Unstructured) error {
	copy := obj.DeepCopy()
	scrubObject(copy.Object)
	for _, field := range []string{"data", "binaryData", "stringData"} {
		if v, ok, _ := unstructured.NestedMap(copy.Object, field); ok && len(v) > 0 {
			return errors.New("Secret data could not be scrubbed safely; Ask AI refuses to send this resource")
		}
	}
	return nil
}
