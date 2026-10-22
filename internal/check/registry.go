package check

// All returns every built-in check in execution order.
func All() []Checker {
	return []Checker{
		NodeCheck{},
		WorkloadCheck{},
		PodCheck{},
		PVCCheck{},
		ServiceCheck{},
		CertificateCheck{},
		PodSecurityCheck{},
		RBACCheck{},
	}
}
