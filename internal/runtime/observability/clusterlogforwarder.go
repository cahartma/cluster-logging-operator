package observability

import (
	obsv1 "github.com/openshift/cluster-logging-operator/api/observability/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Initializer is a function that knows how to initialize a kubernetes runtime object
type Initializer func(o runtime.Object, namespace, name string, visitors ...func(o runtime.Object))

// NewClusterLogForwarder returns a ClusterLogForwarder with name and namespace.
func NewClusterLogForwarder(namespace, name string, initialize Initializer, visitors ...func(clf *obsv1.ClusterLogForwarder)) *obsv1.ClusterLogForwarder {
	clf := &obsv1.ClusterLogForwarder{}
	initialize(clf, namespace, name)
	for _, v := range visitors {
		v(clf)
	}
	clf.Spec.ManagementState = obsv1.ManagementStateManaged

	// TODO: from spec
	clf.Spec.Collector.MaxUnavailable = intstr.IntOrString{
		Type:   intstr.String,
		StrVal: "%100",
	}
	return clf
}
