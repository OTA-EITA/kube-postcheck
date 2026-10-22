package check

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RBACCheck looks for cluster-admin granted to subjects outside the
// Kubernetes system identities. A ServiceAccount holding cluster-admin is the
// most common path from a compromised pod to a compromised cluster, so it fails.
type RBACCheck struct{}

func (RBACCheck) Name() string { return "rbac" }
func (RBACCheck) Description() string {
	return "cluster-admin is not bound to ServiceAccounts or non-system users and groups"
}

func (c RBACCheck) Run(ctx context.Context, cs kubernetes.Interface, _ Options) ([]Finding, error) {
	crbs, err := cs.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list clusterrolebindings: %w", err)
	}
	var out []Finding
	for _, b := range crbs.Items {
		if b.RoleRef.Kind != "ClusterRole" || b.RoleRef.Name != "cluster-admin" {
			continue
		}
		res := "clusterrolebinding/" + b.Name
		for _, s := range b.Subjects {
			if isSystemSubject(s) {
				continue
			}
			who := subjectString(s)
			if s.Kind == rbacv1.ServiceAccountKind {
				out = append(out, fail(c.Name(), res, "cluster-admin granted to "+who))
			} else {
				out = append(out, warn(c.Name(), res, "cluster-admin granted to "+who))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, pass(c.Name(), "cluster", "cluster-admin is limited to system subjects"))
	}
	return out, nil
}

func isSystemSubject(s rbacv1.Subject) bool {
	if s.Kind == rbacv1.ServiceAccountKind {
		return s.Namespace == "kube-system"
	}
	return strings.HasPrefix(s.Name, "system:")
}

func subjectString(s rbacv1.Subject) string {
	if s.Kind == rbacv1.ServiceAccountKind {
		return fmt.Sprintf("ServiceAccount %s/%s", s.Namespace, s.Name)
	}
	return s.Kind + " " + s.Name
}
