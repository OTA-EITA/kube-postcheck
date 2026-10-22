package check

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeCheck verifies that every node is Ready, has no resource pressure,
// and that kubelet versions are consistent across the cluster.
type NodeCheck struct{}

func (NodeCheck) Name() string { return "nodes" }
func (NodeCheck) Description() string {
	return "Nodes are Ready, free of pressure conditions and run the same kubelet version"
}

var pressureConditions = []corev1.NodeConditionType{
	corev1.NodeMemoryPressure,
	corev1.NodeDiskPressure,
	corev1.NodePIDPressure,
	corev1.NodeNetworkUnavailable,
}

func (c NodeCheck) Run(ctx context.Context, cs kubernetes.Interface, _ Options) ([]Finding, error) {
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	if len(nodes.Items) == 0 {
		return []Finding{fail(c.Name(), "cluster", "no nodes registered")}, nil
	}

	var out []Finding
	versions := map[string][]string{}
	for _, n := range nodes.Items {
		res := "node/" + n.Name
		versions[n.Status.NodeInfo.KubeletVersion] = append(versions[n.Status.NodeInfo.KubeletVersion], n.Name)

		problems := 0
		ready := conditionStatus(n.Status.Conditions, corev1.NodeReady)
		if ready != corev1.ConditionTrue {
			out = append(out, fail(c.Name(), res, fmt.Sprintf("Ready=%s", orUnknown(ready))))
			problems++
		}
		for _, t := range pressureConditions {
			if conditionStatus(n.Status.Conditions, t) == corev1.ConditionTrue {
				out = append(out, warn(c.Name(), res, string(t)+"=True"))
				problems++
			}
		}
		if n.Spec.Unschedulable {
			out = append(out, warn(c.Name(), res, "node is cordoned (unschedulable)"))
			problems++
		}
		if problems == 0 {
			out = append(out, pass(c.Name(), res, "Ready, kubelet "+n.Status.NodeInfo.KubeletVersion))
		}
	}
	if len(versions) > 1 {
		out = append(out, warn(c.Name(), "cluster", fmt.Sprintf("kubelet version skew: %v", versions)))
	}
	return out, nil
}

func conditionStatus(conds []corev1.NodeCondition, t corev1.NodeConditionType) corev1.ConditionStatus {
	for _, c := range conds {
		if c.Type == t {
			return c.Status
		}
	}
	return ""
}

func orUnknown(s corev1.ConditionStatus) string {
	if s == "" {
		return "Unknown"
	}
	return string(s)
}
