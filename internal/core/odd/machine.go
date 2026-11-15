package odd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const CurrentSchemaVersion = 1

type State string

const (
	StateAuthorize State = "authorize"
	StateExplore   State = "explore"
	StateResolve   State = "resolve"
	StateClassify  State = "classify"
	StateImplement State = "implement"
	StateClose     State = "close"
	StateCompleted State = "completed"
	StateBlocked   State = "blocked"
	StateStopped   State = "stopped"
)

type Event string

const (
	EventAuthorizationResolved   Event = "authorization_resolved"
	EventExplorationCompleted    Event = "exploration_completed"
	EventResolutionCompleted     Event = "resolution_completed"
	EventClassificationCompleted Event = "classification_completed"
	EventImplementationCompleted Event = "implementation_completed"
	EventClosureCompleted        Event = "closure_completed"
	EventBlocked                 Event = "blocked"
	EventResumed                 Event = "resumed"
	EventStopped                 Event = "stopped"
)

type Machine struct {
	SchemaVersion int    `json:"schema_version"`
	WorkflowID    string `json:"workflow_id"`
	State         State  `json:"state"`
	ResumeState   State  `json:"resume_state,omitempty"`
	Revision      uint64 `json:"revision"`
}

var forwardTransitions = map[State]struct {
	event Event
	next  State
}{
	StateAuthorize: {event: EventAuthorizationResolved, next: StateExplore},
	StateExplore:   {event: EventExplorationCompleted, next: StateResolve},
	StateResolve:   {event: EventResolutionCompleted, next: StateClassify},
	StateClassify:  {event: EventClassificationCompleted, next: StateImplement},
	StateImplement: {event: EventImplementationCompleted, next: StateClose},
	StateClose:     {event: EventClosureCompleted, next: StateCompleted},
}

func New(workflowID string) (Machine, error) {
	machine := Machine{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    workflowID,
		State:         StateAuthorize,
	}
	if err := machine.Validate(); err != nil {
		return Machine{}, err
	}
	return machine, nil
}

func (machine Machine) Apply(event Event) (Machine, error) {
	if err := machine.Validate(); err != nil {
		return machine, fmt.Errorf("invalid machine: %w", err)
	}
	if machine.State.terminal() {
		return machine, fmt.Errorf("state %q is terminal", machine.State)
	}

	if machine.State == StateBlocked {
		switch event {
		case EventResumed:
			machine.State = machine.ResumeState
			machine.ResumeState = ""
			machine.Revision++
			return machine, nil
		case EventStopped:
			machine.State = StateStopped
			machine.ResumeState = ""
			machine.Revision++
			return machine, nil
		default:
			return machine, fmt.Errorf("cannot apply event %q while state is %q", event, machine.State)
		}
	}

	switch event {
	case EventBlocked:
		machine.ResumeState = machine.State
		machine.State = StateBlocked
		machine.Revision++
		return machine, nil
	case EventStopped:
		machine.State = StateStopped
		machine.Revision++
		return machine, nil
	}

	transition := forwardTransitions[machine.State]
	if event != transition.event {
		return machine, fmt.Errorf("cannot apply event %q while state is %q", event, machine.State)
	}

	machine.State = transition.next
	machine.Revision++
	return machine, nil
}

func (machine Machine) Validate() error {
	if machine.SchemaVersion == 0 {
		return errors.New("schema_version is required")
	}
	if machine.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; supported version is %d", machine.SchemaVersion, CurrentSchemaVersion)
	}
	if strings.TrimSpace(machine.WorkflowID) == "" {
		return errors.New("workflow_id is required")
	}
	if !machine.State.valid() {
		return fmt.Errorf("state %q is invalid", machine.State)
	}
	if machine.State == StateBlocked {
		if !machine.ResumeState.active() {
			return errors.New("resume_state is required and must reference an active ODD state")
		}
	} else if machine.ResumeState != "" {
		return errors.New("resume_state is only valid while state is blocked")
	}

	return nil
}

func Decode(reader io.Reader) (Machine, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var machine Machine
	if err := decoder.Decode(&machine); err != nil {
		return Machine{}, fmt.Errorf("decode ODD machine: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Machine{}, errors.New("multiple JSON values are not supported")
	} else if !errors.Is(err, io.EOF) {
		return Machine{}, fmt.Errorf("decode trailing JSON: %w", err)
	}

	if err := machine.Validate(); err != nil {
		return Machine{}, err
	}
	return machine, nil
}

func Marshal(machine Machine) ([]byte, error) {
	if err := machine.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(machine, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func (state State) active() bool {
	switch state {
	case StateAuthorize, StateExplore, StateResolve, StateClassify, StateImplement, StateClose:
		return true
	default:
		return false
	}
}

func (state State) terminal() bool {
	return state == StateCompleted || state == StateStopped
}

func (state State) valid() bool {
	return state.active() || state == StateCompleted || state == StateBlocked || state == StateStopped
}
