package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func service(ns, name string, typ corev1.ServiceType, ingress ...corev1.LoadBalancerIngress) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.ServiceSpec{Type: typ},
		Status:     corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: ingress}},
	}
}

func TestServiceCheck(t *testing.T) {
	fs := runCheck(t, ServiceCheck{}, testOptions(),
		service("ingress", "traefik", corev1.ServiceTypeLoadBalancer, corev1.LoadBalancerIngress{IP: "10.0.0.10"}),
		service("ingress", "alb", corev1.ServiceTypeLoadBalancer, corev1.LoadBalancerIngress{Hostname: "lb.example.com"}),
		service("ingress", "pending", corev1.ServiceTypeLoadBalancer),
		service("app", "internal", corev1.ServiceTypeClusterIP),
	)
	if f := expectStatus(t, fs, "service/ingress/traefik", StatusPass); f.Message != "external address 10.0.0.10" {
		t.Errorf("message = %q", f.Message)
	}
	if f := expectStatus(t, fs, "service/ingress/alb", StatusPass); f.Message != "external address lb.example.com" {
		t.Errorf("hostname should be used when there is no IP, got %q", f.Message)
	}
	expectStatus(t, fs, "service/ingress/pending", StatusFail)
	if _, ok := byResource(fs)["service/app/internal"]; ok {
		t.Error("ClusterIP services should be ignored")
	}
}
