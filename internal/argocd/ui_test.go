package argocd

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func service(ports ...corev1.ServicePort) corev1.Service {
	return corev1.Service{Spec: corev1.ServiceSpec{Ports: ports}}
}

func TestWebPortPrefersTheNameOverTheNumber(t *testing.T) {
	// A chart that moves 80 to 8080 keeps the name, so the name is what the
	// button follows.
	moved := service(
		corev1.ServicePort{Name: "metrics", Port: 8083},
		corev1.ServicePort{Name: "http", Port: 8080},
		corev1.ServicePort{Name: "https", Port: 8443},
	)
	port, err := webPort(moved)
	if err != nil {
		t.Fatalf("webPort: %v", err)
	}
	if port != 8080 {
		t.Fatalf("port = %d, want the named http port", port)
	}

	// Both names reach the same backend, so an installation serving only
	// https is opened there rather than refused.
	tlsOnly := service(corev1.ServicePort{Name: "https", Port: 443})
	if port, err = webPort(tlsOnly); err != nil || port != 443 {
		t.Fatalf("port = %d err = %v", port, err)
	}

	// An unnamed port is still a port worth trying.
	unnamed := service(corev1.ServicePort{Port: 80})
	if port, err = webPort(unnamed); err != nil || port != 80 {
		t.Fatalf("port = %d err = %v", port, err)
	}

	if _, err = webPort(service()); err == nil {
		t.Fatal("a Service with no ports cannot be opened")
	}
}
