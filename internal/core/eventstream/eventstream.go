package eventstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const CurrentSchemaVersion = 1

type Kind string

const (
	KindEvent      Kind = "event"
	KindDiagnostic Kind = "diagnostic"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Record struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	WorkflowID    string      `json:"workflow_id"`
	Sequence      uint64      `json:"sequence"`
	OccurredAt    time.Time   `json:"occurred_at"`
	Kind          Kind        `json:"kind"`
	Event         *Event      `json:"event,omitempty"`
	Diagnostic    *Diagnostic `json:"diagnostic,omitempty"`
}

type Event struct {
	Name      string `json:"name"`
	Message   string `json:"message"`
	State     string `json:"state,omitempty"`
	HandoffID string `json:"handoff_id,omitempty"`
}

type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Detail   string   `json:"detail,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

type Writer struct {
	encoder *json.Encoder
}

type Decoder struct {
	decoder *json.Decoder
}

func NewWriter(output io.Writer) *Writer {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return &Writer{encoder: encoder}
}

func (writer *Writer) Write(record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if err := writer.encoder.Encode(record); err != nil {
		return fmt.Errorf("encode event stream record: %w", err)
	}
	return nil
}

func NewDecoder(input io.Reader) *Decoder {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	return &Decoder{decoder: decoder}
}

func (decoder *Decoder) Next() (Record, error) {
	var record Record
	if err := decoder.decoder.Decode(&record); err != nil {
		if errors.Is(err, io.EOF) {
			return Record{}, io.EOF
		}
		return Record{}, fmt.Errorf("decode event stream record: %w", err)
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (record Record) Validate() error {
	if record.SchemaVersion == 0 {
		return errors.New("schema_version is required")
	}
	if record.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; supported version is %d", record.SchemaVersion, CurrentSchemaVersion)
	}
	if strings.TrimSpace(record.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(record.WorkflowID) == "" {
		return errors.New("workflow_id is required")
	}
	if record.Sequence == 0 {
		return errors.New("sequence must be greater than zero")
	}
	if record.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}

	switch record.Kind {
	case KindEvent:
		if record.Event == nil || record.Diagnostic != nil {
			return errors.New("event record must contain only event payload")
		}
		return record.Event.validate()
	case KindDiagnostic:
		if record.Diagnostic == nil || record.Event != nil {
			return errors.New("diagnostic record must contain only diagnostic payload")
		}
		return record.Diagnostic.validate()
	default:
		return fmt.Errorf("kind %q is invalid", record.Kind)
	}
}

func (event Event) validate() error {
	if strings.TrimSpace(event.Name) == "" {
		return errors.New("event.name is required")
	}
	if strings.TrimSpace(event.Message) == "" {
		return errors.New("event.message is required")
	}
	return nil
}

func (diagnostic Diagnostic) validate() error {
	if strings.TrimSpace(diagnostic.Code) == "" {
		return errors.New("diagnostic.code is required")
	}
	if !diagnostic.Severity.valid() {
		return fmt.Errorf("diagnostic.severity %q is invalid", diagnostic.Severity)
	}
	if strings.TrimSpace(diagnostic.Message) == "" {
		return errors.New("diagnostic.message is required")
	}
	return nil
}

func (severity Severity) valid() bool {
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityError:
		return true
	default:
		return false
	}
}
