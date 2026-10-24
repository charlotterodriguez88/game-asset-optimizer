package gameassets

import "testing"

func TestDecideQueue(t *testing.T) {
	tests := []struct {
		name      string
		request   AssetRequest
		wantState string
		wantQueue string
	}{
		{name: "approved live event art jumps to live queue", request: AssetRequest{Moderation: ModerationApproved, EventID: "raid-9"}, wantState: "ready_to_optimize", wantQueue: "live"},
		{name: "approved profile art uses normal queue", request: AssetRequest{Moderation: ModerationApproved}, wantState: "ready_to_optimize", wantQueue: "normal"},
		{name: "pending player upload waits for review", request: AssetRequest{Moderation: ModerationPending}, wantState: "moderation_queued", wantQueue: "normal"},
		{name: "rejected player upload is blocked", request: AssetRequest{Moderation: ModerationRejected}, wantState: "blocked", wantQueue: "none"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DecideQueue(test.request)
			if got.State != test.wantState || got.Priority != test.wantQueue {
				t.Fatalf("DecideQueue() = state %q priority %q, want %q %q", got.State, got.Priority, test.wantState, test.wantQueue)
			}
		})
	}
}
