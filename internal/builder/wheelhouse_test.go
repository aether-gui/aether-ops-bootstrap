package builder

import (
	"reflect"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestPlanWheelhouseRequirementsPrefersBundledDistroPackages(t *testing.T) {
	plan := PlanWheelhouseRequirements(
		[]string{
			"docker==7.1.0",
			"docker-compose",
			"openshift==0.12.1",
			"requests==2.31.0",
		},
		[]bundle.DebSpec{
			{Name: "python3-docker"},
			{Name: "python3-compose"},
			{Name: "python3-openshift"},
		},
	)

	wantRequirements := []string{"requests==2.31.0"}
	if !reflect.DeepEqual(plan.Requirements, wantRequirements) {
		t.Fatalf("Requirements = %#v, want %#v", plan.Requirements, wantRequirements)
	}

	wantSatisfied := []string{
		"docker-compose via python3-compose",
		"docker==7.1.0 via python3-docker",
		"openshift==0.12.1 via python3-openshift",
	}
	if !reflect.DeepEqual(plan.DistroSatisfiedPip, wantSatisfied) {
		t.Fatalf("DistroSatisfiedPip = %#v, want %#v", plan.DistroSatisfiedPip, wantSatisfied)
	}
}

func TestPlanWheelhouseRequirementsKeepsPipWhenNoBundledDebExists(t *testing.T) {
	plan := PlanWheelhouseRequirements(
		[]string{"docker==7.1.0", "requests==2.31.0"},
		[]bundle.DebSpec{{Name: "python3-pip"}},
	)

	wantRequirements := []string{"docker==7.1.0", "requests==2.31.0"}
	if !reflect.DeepEqual(plan.Requirements, wantRequirements) {
		t.Fatalf("Requirements = %#v, want %#v", plan.Requirements, wantRequirements)
	}

	if len(plan.DistroSatisfiedPip) != 0 {
		t.Fatalf("DistroSatisfiedPip = %#v, want empty", plan.DistroSatisfiedPip)
	}
}
