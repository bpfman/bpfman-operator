package testutil

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ContainsCondition checks if the given condition exists in the conditions slice
// by comparing type, status, reason, and message fields.
func ContainsCondition(conditions []metav1.Condition, condition metav1.Condition) bool {
	for _, c := range conditions {
		if c.Type == condition.Type &&
			c.Status == condition.Status &&
			c.Reason == condition.Reason &&
			c.Message == condition.Message {
			return true
		}
	}

	return false
}
