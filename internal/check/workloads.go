package check

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WorkloadCheck verifies that Deployments, StatefulSets and DaemonSets
// have all of their desired replicas available.
type WorkloadCheck struct{}

func (WorkloadCheck) Name() string { return "workloads" }
func (WorkloadCheck) Description() string {
	return "Deployments, StatefulSets and DaemonSets have all desired replicas available"
}

func (c WorkloadCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	var out []Finding

	deps, err := cs.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deps.Items {
		if !opt.inScope(d.Namespace) {
			continue
		}
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		out = append(out, replicaFinding(c.Name(), "deployment/"+d.Namespace+"/"+d.Name, want, d.Status.AvailableReplicas))
	}

	sts, err := cs.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for _, s := range sts.Items {
		if !opt.inScope(s.Namespace) {
			continue
		}
		want := int32(1)
		if s.Spec.Replicas != nil {
			want = *s.Spec.Replicas
		}
		out = append(out, replicaFinding(c.Name(), "statefulset/"+s.Namespace+"/"+s.Name, want, s.Status.AvailableReplicas))
	}

	dss, err := cs.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	for _, d := range dss.Items {
		if !opt.inScope(d.Namespace) {
			continue
		}
		out = append(out, replicaFinding(c.Name(), "daemonset/"+d.Namespace+"/"+d.Name, d.Status.DesiredNumberScheduled, d.Status.NumberAvailable))
	}
	return out, nil
}

// replicaFinding fails when nothing is available, and warns when the workload is degraded.
func replicaFinding(check, res string, want, available int32) Finding {
	msg := fmt.Sprintf("%d/%d available", available, want)
	switch {
	case want == 0:
		return pass(check, res, "scaled to zero")
	case available >= want:
		return pass(check, res, msg)
	case available == 0:
		return fail(check, res, msg)
	default:
		return warn(check, res, msg)
	}
}
