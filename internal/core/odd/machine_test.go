package odd

import (
	"bytes"
	"strings"
	"testing"
)

func TestMachineFollowsODDHappyPath(t *testing.T) {
	t.Parallel()

	machine, err := New("workflow-1")
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		event Event
		state State
	}{
		{event: EventAuthorizationResolved, state: StateExplore},
		{event: EventExplorationCompleted, state: StateResolve},
		{event: EventResolutionCompleted, state: StateClassify},
		{event: EventClassificationCompleted, state: StateImplement},
		{event: EventImplementationCompleted, state: StateClose},
		{event: EventClosureCompleted, state: StateCompleted},
	}

	for index, step := range steps {
		machine, err = machine.Apply(step.event)
		if err != nil {
			t.Fatalf("apply %s: %v", step.event, err)
		}
		if machine.State != step.state {
			t.Fatalf("after %s expected %s, got %s", step.event, step.state, machine.State)
		}
		if machine.Revision != uint64(index+1) {
			t.Fatalf("after %s expected revision %d, got %d", step.event, index+1, machine.Revision)
		}
	}

	next, err := machine.Apply(EventStopped)
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected completed workflow to be terminal, got %v", err)
	}
	if next != machine {
		t.Fatalf("terminal transition mutated completed machine: before=%#v after=%#v", machine, next)
	}
}

func TestMachineRejectsOutOfOrderEventWithoutMutation(t *testing.T) {
	t.Parallel()

	machine, err := New("workflow-1")
	if err != nil {
		t.Fatal(err)
	}

	next, err := machine.Apply(EventImplementationCompleted)
	if err == nil || !strings.Contains(err.Error(), "cannot apply") {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
	if next != machine {
		t.Fatalf("invalid transition mutated machine: before=%#v after=%#v", machine, next)
	}
}

func TestMachineCanBlockAndResumeEveryActiveState(t *testing.T) {
	t.Parallel()

	activeStates := []State{
		StateAuthorize,
		StateExplore,
		StateResolve,
		StateClassify,
		StateImplement,
		StateClose,
	}

	for _, state := range activeStates {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			machine := Machine{
				SchemaVersion: CurrentSchemaVersion,
				WorkflowID:    "workflow-1",
				State:         state,
				Revision:      3,
			}
			blocked, err := machine.Apply(EventBlocked)
			if err != nil {
				t.Fatalf("block %s: %v", state, err)
			}
			if blocked.State != StateBlocked || blocked.ResumeState != state || blocked.Revision != 4 {
				t.Fatalf("unexpected blocked machine: %#v", blocked)
			}

			resumed, err := blocked.Apply(EventResumed)
			if err != nil {
				t.Fatalf("resume %s: %v", state, err)
			}
			if resumed.State != state || resumed.ResumeState != "" || resumed.Revision != 5 {
				t.Fatalf("unexpected resumed machine: %#v", resumed)
			}
		})
	}
}

func TestMachineStopIsTerminal(t *testing.T) {
	t.Parallel()

	machine, err := New("workflow-1")
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := machine.Apply(EventStopped)
	if err != nil {
		t.Fatalf("stop machine: %v", err)
	}
	if stopped.State != StateStopped {
		t.Fatalf("expected stopped state, got %s", stopped.State)
	}

	next, err := stopped.Apply(EventAuthorizationResolved)
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected terminal state error, got %v", err)
	}
	if next != stopped {
		t.Fatalf("terminal transition mutated machine: before=%#v after=%#v", stopped, next)
	}
}

func TestBlockedMachineCanStop(t *testing.T) {
	t.Parallel()

	machine, err := New("workflow-1")
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := machine.Apply(EventBlocked)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := blocked.Apply(EventStopped)
	if err != nil {
		t.Fatalf("stop blocked machine: %v", err)
	}
	if stopped.State != StateStopped || stopped.ResumeState != "" || stopped.Revision != 2 {
		t.Fatalf("unexpected stopped machine: %#v", stopped)
	}
}

func TestMachineSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	machine, err := New("workflow-1")
	if err != nil {
		t.Fatal(err)
	}
	machine, err = machine.Apply(EventAuthorizationResolved)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := Marshal(machine)
	if err != nil {
		t.Fatalf("marshal machine: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode machine: %v", err)
	}
	if decoded != machine {
		t.Fatalf("unexpected decoded machine: before=%#v after=%#v", machine, decoded)
	}
}

func TestDecodeRejectsInvalidSnapshots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "missing workflow identity",
			input:    `{"schema_version":1,"workflow_id":"","state":"authorize","revision":0}`,
			expected: "workflow_id is required",
		},
		{
			name:     "unknown field",
			input:    `{"schema_version":1,"workflow_id":"workflow-1","state":"authorize","revision":0,"provider":"claude"}`,
			expected: "unknown field",
		},
		{
			name:     "unsupported version",
			input:    `{"schema_version":2,"workflow_id":"workflow-1","state":"authorize","revision":0}`,
			expected: "unsupported schema_version 2",
		},
		{
			name:     "invalid state",
			input:    `{"schema_version":1,"workflow_id":"workflow-1","state":"planning","revision":0}`,
			expected: "state",
		},
		{
			name:     "blocked without resume state",
			input:    `{"schema_version":1,"workflow_id":"workflow-1","state":"blocked","revision":1}`,
			expected: "resume_state is required",
		},
		{
			name:     "resume state outside blocked state",
			input:    `{"schema_version":1,"workflow_id":"workflow-1","state":"explore","resume_state":"authorize","revision":1}`,
			expected: "resume_state is only valid",
		},
		{
			name:     "multiple snapshots",
			input:    `{"schema_version":1,"workflow_id":"workflow-1","state":"authorize","revision":0} {}`,
			expected: "multiple JSON values",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}
