package check

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PVCCheck verifies that PersistentVolumeClaims are Bound and that a default
// StorageClass exists, which is a common gap right after building a cluster.
type PVCCheck struct{}

func (PVCCheck) Name() string { return "storage" }
func (PVCCheck) Description() string {
	return "PersistentVolumeClaims are Bound and exactly one default StorageClass exists"
}

const defaultClassAnnotation = "storageclass.kubernetes.io/is-default-class"

func (c PVCCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	var out []Finding

	scs, err := cs.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list storageclasses: %w", err)
	}
	var defaults []string
	for _, sc := range scs.Items {
		if sc.Annotations[defaultClassAnnotation] == "true" {
			defaults = append(defaults, sc.Name)
		}
	}
	switch len(defaults) {
	case 0:
		out = append(out, warn(c.Name(), "cluster", "no default StorageClass; PVCs without storageClassName will stay Pending"))
	case 1:
		out = append(out, pass(c.Name(), "storageclass/"+defaults[0], "default StorageClass"))
	default:
		out = append(out, warn(c.Name(), "cluster", fmt.Sprintf("multiple default StorageClasses: %v", defaults)))
	}

	pvcs, err := cs.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pvcs: %w", err)
	}
	for _, p := range pvcs.Items {
		if !opt.inScope(p.Namespace) {
			continue
		}
		res := "pvc/" + p.Namespace + "/" + p.Name
		switch p.Status.Phase {
		case corev1.ClaimBound:
			out = append(out, pass(c.Name(), res, "Bound"))
		case corev1.ClaimLost:
			out = append(out, fail(c.Name(), res, "Lost: the bound PersistentVolume no longer exists"))
		default:
			out = append(out, fail(c.Name(), res, "phase "+string(p.Status.Phase)))
		}
	}
	return out, nil
}
