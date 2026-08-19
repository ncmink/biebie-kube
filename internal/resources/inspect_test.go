package resources

import (
	"encoding/base64"
	"testing"

	"biebie-kube/internal/domain"
)

func TestInspectSecretKeepsBase64AndDoesNotDecode(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hunter2"))
	secret := object(t, `{
		"metadata": {"name": "argocd-redis", "namespace": "argo-ns"},
		"type": "Opaque",
		"data": {"auth": "`+encoded+`"}
	}`)

	got := Inspect(domain.KindSecret, secret)
	if got.Type != "Opaque" {
		t.Fatalf("type = %q", got.Type)
	}
	if len(got.Data) != 1 || got.Data[0].Key != "auth" {
		t.Fatalf("data = %+v", got.Data)
	}
	if got.Data[0].Value != encoded {
		t.Fatalf("value = %q, want the stored base64 %q — must not decode", got.Data[0].Value, encoded)
	}
	if got.Data[0].Value == "hunter2" {
		t.Fatal("plaintext leaked into the inspect payload")
	}
}

func TestInspectSecretReencodesTypedBytesAsBase64(t *testing.T) {
	secret := object(t, `{"metadata": {"name": "tls"}}`)
	secret.Object["data"] = map[string]any{
		"tls.key": []byte("BEGIN PRIVATE KEY"),
	}

	got := Inspect(domain.KindSecret, secret)
	if len(got.Data) != 1 {
		t.Fatalf("data = %+v", got.Data)
	}
	want := base64.StdEncoding.EncodeToString([]byte("BEGIN PRIVATE KEY"))
	if got.Data[0].Value != want {
		t.Fatalf("value = %q, want re-encoded base64 %q", got.Data[0].Value, want)
	}
	if got.Data[0].Value == "BEGIN PRIVATE KEY" {
		t.Fatal("typed []byte was passed through as plaintext")
	}
}

func TestInspectConfigMapKeepsPlainDataAndBinaryAsStored(t *testing.T) {
	cm := object(t, `{
		"metadata": {"name": "app"},
		"data": {"config.yaml": "listen: :8080"},
		"binaryData": {"blob": "AQID"}
	}`)

	got := Inspect(domain.KindConfigMap, cm)
	if len(got.Data) != 2 {
		t.Fatalf("data = %+v", got.Data)
	}
	byKey := map[string]domain.DataEntry{}
	for _, entry := range got.Data {
		byKey[entry.Key] = entry
	}
	if byKey["config.yaml"].Value != "listen: :8080" || byKey["config.yaml"].Binary {
		t.Fatalf("plain data = %+v", byKey["config.yaml"])
	}
	if byKey["blob"].Value != "AQID" || !byKey["blob"].Binary {
		t.Fatalf("binary data = %+v", byKey["blob"])
	}
}

func TestInspectOtherKindsHaveNoData(t *testing.T) {
	pod := object(t, `{"metadata": {"name": "api"}}`)
	got := Inspect(domain.KindPod, pod)
	if len(got.Data) != 0 {
		t.Fatalf("pod inspect leaked data: %+v", got.Data)
	}
}
