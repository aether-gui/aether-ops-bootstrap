package components

import (
	"context"
	"log"
)

// ApplyPlan runs every Action in the plan sequentially, logging each
// action's Description before invoking it. Components use this as the
// default Apply body so users see "what is happening now" rather than
// only "what just completed".
//
// The log prefix mirrors the launcher's per-component format:
//
//	[component] -> action description
func ApplyPlan(ctx context.Context, componentName string, plan Plan) error {
	for _, action := range plan.Actions {
		if action.Description != "" {
			log.Printf("[%s] -> %s", componentName, action.Description)
		}
		if err := action.Fn(ctx); err != nil {
			return err
		}
	}
	return nil
}
