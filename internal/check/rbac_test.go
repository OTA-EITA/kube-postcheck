package check

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func adminBinding(name string, subjects ...rbacv1.Subject) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   subjects,
	}
}

func TestRBACCheck(t *testing.T) {
	viewer := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-view"},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "view"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: "ci", Name: "runner"}},
	}
	fs := runCheck(t, RBACCheck{}, testOptions(),
		adminBinding("cluster-admin", rbacv1.Subject{Kind: rbacv1.GroupKind, Name: "system:masters"}),
		adminBinding("argocd", rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Namespace: "argocd", Name: "argocd-server"}),
		adminBinding("ops", rbacv1.Subject{Kind: rbacv1.UserKind, Name: "alice@example.com"}),
		viewer,
	)
	expectStatus(t, fs, "clusterrolebinding/argocd", StatusFail)
	expectStatus(t, fs, "clusterrolebinding/ops", StatusWarn)
	for _, f := range fs {
		if f.Resource == "clusterrolebinding/cluster-admin" || f.Resource == "clusterrolebinding/ci-view" {
			t.Errorf("unexpected finding %+v", f)
		}
	}
}

func TestRBACCheckClean(t *testing.T) {
	fs := runCheck(t, RBACCheck{}, testOptions(),
		adminBinding("cluster-admin", rbacv1.Subject{Kind: rbacv1.GroupKind, Name: "system:masters"}),
		adminBinding("kube-system-sa", rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Namespace: "kube-system", Name: "clusterrole-aggregation-controller"}),
	)
	expectStatus(t, fs, "cluster", StatusPass)
}
