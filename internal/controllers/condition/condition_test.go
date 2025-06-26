package condition

import (
	"testing"
	"time"

	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPending(t *testing.T) {
	beforeCall := time.Now()
	result := Pending()
	afterCall := time.Now()

	// Test Type
	if result.Type != condition.TypeReady {
		t.Errorf("Expected Type to be %s, got %s", condition.TypeReady, result.Type)
	}

	// Test Status
	if result.Status != metav1.ConditionFalse {
		t.Errorf("Expected Status to be %s, got %s", metav1.ConditionFalse, result.Status)
	}

	// Test Reason
	if result.Reason != ReasonPending {
		t.Errorf("Expected Reason to be %s, got %s", ReasonPending, result.Reason)
	}

	// Test LastTransitionTime is within reasonable bounds
	transitionTime := result.LastTransitionTime.Time
	if transitionTime.Before(beforeCall) || transitionTime.After(afterCall) {
		t.Errorf("Expected LastTransitionTime to be between %v and %v, got %v", beforeCall, afterCall, transitionTime)
	}
}

func TestReasonPendingConstant(t *testing.T) {
	expected := "Pending"
	if ReasonPending != expected {
		t.Errorf("Expected ReasonPending to be %s, got %s", expected, ReasonPending)
	}
}
