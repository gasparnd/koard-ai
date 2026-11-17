package memory_test

import (
	"context"
	"os"
	"testing"

	"github.com/koard-ai/koard/internal/core/memory"
)

func TestStatusReadsHistoryWithoutSaving(t *testing.T) {
	for _, state := range []string{"automatic", "pending", "saved", "declined", "unknown"} {
		t.Run(state, func(t *testing.T) {
			c, s, in := fixture(t)
			if state == "pending" || state == "declined" {
				in.Result.MemoryProposals[0].Uncertainty = []string{"Confirm."}
			}
			if state == "saved" || state == "unknown" {
				s.foreign = state == "unknown"
				_, _ = c.Execute(context.Background(), in, nil)
			}
			if state == "declined" {
				plan, err := c.Assess(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Execute(context.Background(), in, &memory.Decision{PlanID: plan.ID, Answer: "declined"}); err != nil {
					t.Fatal(err)
				}
			}
			before := s.calls
			plan, out, err := c.Status(context.Background(), in)
			if err != nil || out.Status != state || plan.ID == "" || s.calls != before {
				t.Fatalf("%+v %+v %v creates=%d", plan, out, err, s.calls)
			}
			if state == "automatic" || state == "pending" {
				if _, err := os.Stat(c.JournalDir); !os.IsNotExist(err) {
					t.Fatal("read-only status created state")
				}
			}
		})
	}
}
