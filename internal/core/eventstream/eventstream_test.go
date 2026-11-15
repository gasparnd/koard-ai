package eventstream

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriterProducesDecodableJSONLines(t *testing.T) {
	t.Parallel()

	records := []Record{
		validEventRecord(),
		validDiagnosticRecord(),
	}
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range records {
		if err := writer.Write(record); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}

	if lines := strings.Count(output.String(), "\n"); lines != len(records) {
		t.Fatalf("expected %d JSON lines, got %d: %q", len(records), lines, output.String())
	}

	decoder := NewDecoder(&output)
	for index, expected := range records {
		actual, err := decoder.Next()
		if err != nil {
			t.Fatalf("decode record %d: %v", index, err)
		}
		if !actual.OccurredAt.Equal(expected.OccurredAt) {
			t.Fatalf("record %d timestamp changed: expected %s, got %s", index, expected.OccurredAt, actual.OccurredAt)
		}
		actual.OccurredAt = expected.OccurredAt
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("record %d changed: expected %#v, got %#v", index, expected, actual)
		}
	}
	if _, err := decoder.Next(); err != io.EOF {
		t.Fatalf("expected EOF after records, got %v", err)
	}
}

func TestRecordValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Record)
		expected string
	}{
		{
			name: "unsupported schema version",
			mutate: func(record *Record) {
				record.SchemaVersion = 2
			},
			expected: "unsupported schema_version 2",
		},
		{
			name: "missing record identity",
			mutate: func(record *Record) {
				record.ID = ""
			},
			expected: "id is required",
		},
		{
			name: "missing workflow identity",
			mutate: func(record *Record) {
				record.WorkflowID = ""
			},
			expected: "workflow_id is required",
		},
		{
			name: "missing sequence",
			mutate: func(record *Record) {
				record.Sequence = 0
			},
			expected: "sequence must be greater than zero",
		},
		{
			name: "missing timestamp",
			mutate: func(record *Record) {
				record.OccurredAt = time.Time{}
			},
			expected: "occurred_at is required",
		},
		{
			name: "event with diagnostic payload",
			mutate: func(record *Record) {
				record.Diagnostic = validDiagnosticRecord().Diagnostic
			},
			expected: "event record must contain only event payload",
		},
		{
			name: "missing event name",
			mutate: func(record *Record) {
				record.Event.Name = ""
			},
			expected: "event.name is required",
		},
		{
			name: "missing event message",
			mutate: func(record *Record) {
				record.Event.Message = ""
			},
			expected: "event.message is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			record := validEventRecord()
			test.mutate(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestDiagnosticValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Record)
		expected string
	}{
		{
			name: "diagnostic with event payload",
			mutate: func(record *Record) {
				record.Event = validEventRecord().Event
			},
			expected: "diagnostic record must contain only diagnostic payload",
		},
		{
			name: "invalid severity",
			mutate: func(record *Record) {
				record.Diagnostic.Severity = Severity("critical")
			},
			expected: "diagnostic.severity",
		},
		{
			name: "missing code",
			mutate: func(record *Record) {
				record.Diagnostic.Code = ""
			},
			expected: "diagnostic.code is required",
		},
		{
			name: "missing message",
			mutate: func(record *Record) {
				record.Diagnostic.Message = ""
			},
			expected: "diagnostic.message is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			record := validDiagnosticRecord()
			test.mutate(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestDecoderRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	input := `{"schema_version":1,"id":"record-1","workflow_id":"workflow-1","sequence":1,"occurred_at":"2026-11-15T12:00:00Z","kind":"event","provider":"claude","event":{"name":"workflow.transitioned","message":"Workflow advanced."}}`
	_, err := NewDecoder(strings.NewReader(input)).Next()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestWriterRejectsInvalidRecordWithoutOutput(t *testing.T) {
	t.Parallel()

	record := validEventRecord()
	record.Event = nil
	var output bytes.Buffer
	err := NewWriter(&output).Write(record)
	if err == nil {
		t.Fatal("expected invalid record error")
	}
	if output.Len() != 0 {
		t.Fatalf("invalid record produced output: %q", output.String())
	}
}

func validEventRecord() Record {
	return Record{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "record-1",
		WorkflowID:    "workflow-1",
		Sequence:      1,
		OccurredAt:    time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC),
		Kind:          KindEvent,
		Event: &Event{
			Name:    "workflow.transitioned",
			Message: "Workflow advanced from authorize to explore.",
			State:   "explore",
		},
	}
}

func validDiagnosticRecord() Record {
	return Record{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "record-2",
		WorkflowID:    "workflow-1",
		Sequence:      2,
		OccurredAt:    time.Date(2026, time.November, 15, 12, 0, 1, 0, time.UTC),
		Kind:          KindDiagnostic,
		Diagnostic: &Diagnostic{
			Code:     "KOARD_CONFIG_INVALID",
			Severity: SeverityError,
			Message:  "Project configuration is invalid.",
			Detail:   "schema_version 2 is not supported",
			Hint:     "Use schema_version 1.",
		},
	}
}
