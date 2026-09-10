package main

import (
	"fmt"

	"biebie-kube/internal/domain"
)

func (c *Core) requireWrite(clusterID string, cap domain.Capability) error {
	if c.policy == nil {
		return nil
	}
	decision := c.policy.Check(clusterID, cap)
	if decision.Allowed {
		return nil
	}
	if decision.Code != "" && decision.Reason != "" {
		return fmt.Errorf("%s: %s", decision.Code, decision.Reason)
	}
	if decision.Reason != "" {
		return fmt.Errorf("%s", decision.Reason)
	}
	return fmt.Errorf("operation is not allowed")
}
