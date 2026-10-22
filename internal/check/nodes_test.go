package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func node(name, kubelet string, conds ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: conds,
			NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: kubelet},
		},
	}
}

func cond(t corev1.NodeConditionType, s corev1.ConditionStatus) corev1.NodeCondition {
	return corev1.NodeCondition{Type: t, Status: s}
}

func TestNodeCheck(t *testing.T) {
	cordoned := node("cordoned", "v1.34.1", cond(corev1.NodeReady, corev1.ConditionTrue))
	cordoned.Spec.Unschedulable = true

	fs := runCheck(t, NodeCheck{}, testOptions(),
		node("healthy", "v1.34.1", cond(corev1.NodeReady, corev1.ConditionTrue)),
		node("notready", "v1.34.1", cond(corev1.NodeReady, corev1.ConditionFalse)),
		node("disk", "v1.34.1", cond(corev1.NodeReady, corev1.ConditionTrue), cond(corev1.NodeDiskPressure, corev1.ConditionTrue)),
		node("unknown", "v1.34.1"),
		cordoned,
	)
	expectStatus(t, fs, "node/healthy", StatusPass)
	expectStatus(t, fs, "node/notready", StatusFail)
	expectStatus(t, fs, "node/disk", StatusWarn)
	if f := expectStatus(t, fs, "node/unknown", StatusFail); f.Message != "Ready=Unknown" {
		t.Errorf("missing Ready condition should read as Unknown, got %q", f.Message)
	}
	expectStatus(t, fs, "node/cordoned", StatusWarn)
}

func TestNodeCheckVersionSkew(t *testing.T) {
	fs := runCheck(t, NodeCheck{}, testOptions(),
		node("a", "v1.34.1", cond(corev1.NodeReady, corev1.ConditionTrue)),
		node("b", "v1.33.5", cond(corev1.NodeReady, corev1.ConditionTrue)),
	)
	expectStatus(t, fs, "cluster", StatusWarn)
}

func TestNodeCheckNoNodes(t *testing.T) {
	fs := runCheck(t, NodeCheck{}, testOptions())
	expectStatus(t, fs, "cluster", StatusFail)
}
