package check

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func deployment(ns, name string, want, available int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(want)},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: available},
	}
}

func TestWorkloadCheck(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "db", Name: "postgres"},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To[int32](3)},
		Status:     appsv1.StatefulSetStatus{AvailableReplicas: 2},
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "node-exporter"},
		Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberAvailable: 4},
	}
	fs := runCheck(t, WorkloadCheck{}, testOptions(),
		deployment("app", "api", 2, 2),
		deployment("app", "worker", 2, 0),
		deployment("app", "paused", 0, 0),
		sts, ds,
	)
	expectStatus(t, fs, "deployment/app/api", StatusPass)
	expectStatus(t, fs, "deployment/app/worker", StatusFail)
	expectStatus(t, fs, "deployment/app/paused", StatusPass)
	expectStatus(t, fs, "statefulset/db/postgres", StatusWarn)
	expectStatus(t, fs, "daemonset/monitoring/node-exporter", StatusPass)
}

func TestWorkloadCheckRespectsNamespaces(t *testing.T) {
	opt := testOptions()
	opt.Namespaces = []string{"app"}
	fs := runCheck(t, WorkloadCheck{}, opt,
		deployment("app", "api", 1, 1),
		deployment("other", "broken", 1, 0),
	)
	if _, ok := byResource(fs)["deployment/other/broken"]; ok {
		t.Fatal("deployment outside --namespace should be ignored")
	}
}
