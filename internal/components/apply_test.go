package components

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestApplyPlan_RunsActionsInOrder(t *testing.T) {
	var order []string
	plan := Plan{
		Actions: []Action{
			{Description: "one", Fn: func(_ context.Context) error { order = append(order, "one"); return nil }},
			{Description: "two", Fn: func(_ context.Context) error { order = append(order, "two"); return nil }},
			{Description: "three", Fn: func(_ context.Context) error { order = append(order, "three"); return nil }},
		},
	}

	if err := ApplyPlan(context.Background(), "stub", plan); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}

	want := []string{"one", "two", "three"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i, got := range order {
		if got != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestApplyPlan_StopsOnError(t *testing.T) {
	sentinel := errors.New("boom")
	var ran []string

	plan := Plan{
		Actions: []Action{
			{Description: "one", Fn: func(_ context.Context) error { ran = append(ran, "one"); return nil }},
			{Description: "two", Fn: func(_ context.Context) error { ran = append(ran, "two"); return sentinel }},
			{Description: "three", Fn: func(_ context.Context) error { ran = append(ran, "three"); return nil }},
		},
	}

	err := ApplyPlan(context.Background(), "stub", plan)
	if !errors.Is(err, sentinel) {
		t.Fatalf("ApplyPlan err = %v, want wrapping %v", err, sentinel)
	}
	if len(ran) != 2 || ran[0] != "one" || ran[1] != "two" {
		t.Fatalf("ran = %v, want [one two]; third action should have been skipped", ran)
	}
}

func TestApplyPlan_LogsDescriptionBeforeAction(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}()

	var ranBeforeLog bool
	plan := Plan{
		Actions: []Action{
			{
				Description: "waiting for k8s to stabilize",
				Fn: func(_ context.Context) error {
					// Rationale: the "starting step" announcement must land
					// before the action body runs, otherwise the user is
					// still reading "last step completed" while the slow
					// work is already underway.
					if !strings.Contains(buf.String(), "waiting for k8s to stabilize") {
						ranBeforeLog = true
					}
					return nil
				},
			},
		},
	}

	if err := ApplyPlan(context.Background(), "rke2", plan); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if ranBeforeLog {
		t.Fatal("action body ran before its Description was logged")
	}
	logged := buf.String()
	if !strings.Contains(logged, "[rke2]") {
		t.Errorf("expected component name in log prefix, got %q", logged)
	}
	if !strings.Contains(logged, "waiting for k8s to stabilize") {
		t.Errorf("expected action description in log, got %q", logged)
	}
}

func TestApplyPlan_EmptyDescriptionNotLogged(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}()

	plan := Plan{
		Actions: []Action{
			{Description: "", Fn: func(_ context.Context) error { return nil }},
		},
	}

	if err := ApplyPlan(context.Background(), "stub", plan); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for empty description, got %q", buf.String())
	}
}
