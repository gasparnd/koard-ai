package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const CurrentSchemaVersion = 1

type Role string

const (
	RolePlanner  Role = "planner"
	RoleExecutor Role = "executor"
	RoleVerifier Role = "verifier"
)

type Status string

const (
	StatusCompleted Status = "completed"
	StatusBlocked   Status = "blocked"
	StatusFailed    Status = "failed"
)

type Request struct {
	SchemaVersion  int            `json:"schema_version"`
	ID             string         `json:"id"`
	TaskID         string         `json:"task_id"`
	From           Role           `json:"from"`
	To             Role           `json:"to"`
	Objective      string         `json:"objective"`
	Context        []Reference    `json:"context,omitempty"`
	Capabilities   []string       `json:"capabilities,omitempty"`
	ExpectedOutput OutputContract `json:"expected_output"`
}

type Result struct {
	SchemaVersion int         `json:"schema_version"`
	HandoffID     string      `json:"handoff_id"`
	Role          Role        `json:"role"`
	Status        Status      `json:"status"`
	Summary       string      `json:"summary"`
	Artifacts     []Reference `json:"artifacts,omitempty"`
	Evidence      []Reference `json:"evidence,omitempty"`
	Next          *Transition `json:"next,omitempty"`
}

type Reference struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Digest string `json:"digest,omitempty"`
}

type OutputContract struct {
	Format string `json:"format"`
	Schema string `json:"schema,omitempty"`
}

type Transition struct {
	Role   Role   `json:"role"`
	Reason string `json:"reason"`
}

func DecodeRequest(reader io.Reader) (Request, error) {
	request, err := decodeSingle[Request](reader)
	if err != nil {
		return Request{}, fmt.Errorf("decode handoff request: %w", err)
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func DecodeResult(reader io.Reader) (Result, error) {
	result, err := decodeSingle[Result](reader)
	if err != nil {
		return Result{}, fmt.Errorf("decode handoff result: %w", err)
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func MarshalRequest(request Request) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return marshal(request)
}

func MarshalResult(result Result) ([]byte, error) {
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return marshal(result)
}

// RoleOrchestrator is a sender only; it is never a responding model role.
const RoleOrchestrator Role = "orchestrator"

func (request Request) Validate() error {
	if err := validateSchemaVersion(request.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(request.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(request.TaskID) == "" {
		return errors.New("task_id is required")
	}
	if !request.From.valid() && request.From != RoleOrchestrator {
		return fmt.Errorf("from role %q is invalid", request.From)
	}
	if !request.To.valid() {
		return fmt.Errorf("to role %q is invalid", request.To)
	}
	if request.From == request.To {
		return errors.New("from and to roles must be different")
	}
	if strings.TrimSpace(request.Objective) == "" {
		return errors.New("objective is required")
	}
	if err := validateReferences("context", request.Context); err != nil {
		return err
	}
	for index, capability := range request.Capabilities {
		if strings.TrimSpace(capability) == "" {
			return fmt.Errorf("capabilities[%d] is required", index)
		}
	}
	if strings.TrimSpace(request.ExpectedOutput.Format) == "" {
		return errors.New("expected_output.format is required")
	}

	return nil
}

func (result Result) Validate() error {
	if err := validateSchemaVersion(result.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(result.HandoffID) == "" {
		return errors.New("handoff_id is required")
	}
	if !result.Role.valid() {
		return fmt.Errorf("role %q is invalid", result.Role)
	}
	if !result.Status.valid() {
		return fmt.Errorf("status %q is invalid", result.Status)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return errors.New("summary is required")
	}
	if err := validateReferences("artifacts", result.Artifacts); err != nil {
		return err
	}
	if err := validateReferences("evidence", result.Evidence); err != nil {
		return err
	}
	if result.Next != nil {
		if !result.Next.Role.valid() {
			return fmt.Errorf("next.role %q is invalid", result.Next.Role)
		}
		if strings.TrimSpace(result.Next.Reason) == "" {
			return errors.New("next.reason is required")
		}
	}

	return nil
}

func ValidateResultFor(request Request, result Result) error {
	if err := request.Validate(); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if err := result.Validate(); err != nil {
		return fmt.Errorf("invalid result: %w", err)
	}
	if result.HandoffID != request.ID {
		return fmt.Errorf("result handoff_id %q does not match request %q", result.HandoffID, request.ID)
	}
	if result.Role != request.To {
		return fmt.Errorf("result role %q does not match request destination %q", result.Role, request.To)
	}

	return nil
}

func validateSchemaVersion(version int) error {
	if version == 0 {
		return errors.New("schema_version is required")
	}
	if version != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; supported version is %d", version, CurrentSchemaVersion)
	}
	return nil
}

func validateReferences(name string, references []Reference) error {
	for index, reference := range references {
		if strings.TrimSpace(reference.Kind) == "" {
			return fmt.Errorf("%s[%d].kind is required", name, index)
		}
		if strings.TrimSpace(reference.URI) == "" {
			return fmt.Errorf("%s[%d].uri is required", name, index)
		}
	}
	return nil
}

func (role Role) valid() bool {
	switch role {
	case RolePlanner, RoleExecutor, RoleVerifier:
		return true
	default:
		return false
	}
}

func (status Status) valid() bool {
	switch status {
	case StatusCompleted, StatusBlocked, StatusFailed:
		return true
	default:
		return false
	}
}

func decodeSingle[T any](reader io.Reader) (T, error) {
	var value T
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return value, errors.New("multiple JSON values are not supported")
	} else if !errors.Is(err, io.EOF) {
		return value, fmt.Errorf("decode trailing JSON: %w", err)
	}

	return value, nil
}

func marshal(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
