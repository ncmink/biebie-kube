package argocd

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"biebie-kube/internal/domain"
)

// OpenUI returns a local URL that reaches the Argo CD web UI.
//
// The engineer is asked for neither a URL nor a port. The Service is found by
// its label, a port forward to it is reused when one is already running, and
// only the loopback address it produced crosses back — the same tunnel the
// port-forward panel lists, so closing it there closes this too.
//
// The Argo CD server serves HTTP and HTTPS on one backend and redirects to
// HTTPS with a self-signed certificate, so the browser shows one warning. That
// is Argo CD's own default rather than something this application can fix from
// here, and the dashboard says so beside the button.
func (s *Service) OpenUI(ctx context.Context, clusterID string) (domain.ArgoEndpoint, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ArgoEndpoint{}, err
	}
	service, err := serverService(ctx, client)
	if err != nil {
		return domain.ArgoEndpoint{}, err
	}
	port, err := webPort(service)
	if err != nil {
		return domain.ArgoEndpoint{}, err
	}

	if existing, ok := s.forwards.Find(clusterID, service.Namespace, "service", service.Name); ok {
		return domain.ArgoEndpoint{URL: localURL(existing.LocalPort), Reused: true}, nil
	}

	session, err := s.forwards.Start(ctx, clusterID, domain.PortForwardRequest{
		Namespace:    service.Namespace,
		ResourceType: "service",
		ResourceName: service.Name,
		RemotePort:   port,
	})
	if err != nil {
		return domain.ArgoEndpoint{}, err
	}
	return domain.ArgoEndpoint{URL: localURL(session.LocalPort)}, nil
}

// webPort picks the port the UI is served on.
//
// The named port is preferred over its number, because a chart may move 80 to
// 8080 and the name is what stays put. HTTP is preferred over HTTPS: both
// reach the same backend, and asking for the plaintext one means the
// certificate warning comes from Argo CD's redirect rather than from the
// tunnel.
func webPort(service corev1.Service) (int, error) {
	for _, name := range []string{"http", "https", "server"} {
		for _, port := range service.Spec.Ports {
			if port.Name == name {
				return int(port.Port), nil
			}
		}
	}
	if len(service.Spec.Ports) > 0 {
		return int(service.Spec.Ports[0].Port), nil
	}
	return 0, fmt.Errorf("the Argo CD server Service %s declares no ports", service.Name)
}

func localURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }
