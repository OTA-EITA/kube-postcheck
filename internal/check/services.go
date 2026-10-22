package check

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ServiceCheck verifies that LoadBalancer Services received an external address.
type ServiceCheck struct{}

func (ServiceCheck) Name() string { return "services" }
func (ServiceCheck) Description() string {
	return "LoadBalancer Services have been assigned an external address"
}

func (c ServiceCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	svcs, err := cs.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	var out []Finding
	for _, s := range svcs.Items {
		if !opt.inScope(s.Namespace) || s.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		res := "service/" + s.Namespace + "/" + s.Name
		if len(s.Status.LoadBalancer.Ingress) == 0 {
			out = append(out, fail(c.Name(), res, "no external address assigned"))
			continue
		}
		ing := s.Status.LoadBalancer.Ingress[0]
		addr := ing.IP
		if addr == "" {
			addr = ing.Hostname
		}
		out = append(out, pass(c.Name(), res, "external address "+addr))
	}
	return out, nil
}
