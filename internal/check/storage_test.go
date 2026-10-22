package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func storageClass(name string, isDefault bool) *storagev1.StorageClass {
	sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: name}, Provisioner: "rbd.csi.ceph.com"}
	if isDefault {
		sc.Annotations = map[string]string{defaultClassAnnotation: "true"}
	}
	return sc
}

func pvc(ns, name string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
}

func TestPVCCheck(t *testing.T) {
	fs := runCheck(t, PVCCheck{}, testOptions(),
		storageClass("ceph-rbd", true),
		storageClass("cephfs", false),
		pvc("db", "data", corev1.ClaimBound),
		pvc("db", "waiting", corev1.ClaimPending),
		pvc("db", "gone", corev1.ClaimLost),
	)
	expectStatus(t, fs, "storageclass/ceph-rbd", StatusPass)
	expectStatus(t, fs, "pvc/db/data", StatusPass)
	expectStatus(t, fs, "pvc/db/waiting", StatusFail)
	expectStatus(t, fs, "pvc/db/gone", StatusFail)
}

func TestPVCCheckDefaultStorageClass(t *testing.T) {
	none := runCheck(t, PVCCheck{}, testOptions(), storageClass("a", false))
	expectStatus(t, none, "cluster", StatusWarn)

	two := runCheck(t, PVCCheck{}, testOptions(), storageClass("a", true), storageClass("b", true))
	expectStatus(t, two, "cluster", StatusWarn)
}
