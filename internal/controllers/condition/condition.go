package condition

import (
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reasons a resource is or is not ready.
const (
	ReasonPending string = "Pending"
)

// Pending returns a metav1.Condition indicating that a resource is pending. This means that the resource has been created or updated, but is not yet ready for use.
func Pending() metav1.Condition {
	return metav1.Condition{
		Type:               condition.TypeReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonPending,
	}
}
