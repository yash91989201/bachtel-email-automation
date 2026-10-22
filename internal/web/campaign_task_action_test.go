package web

import (
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
)

func TestCampaignTaskActionUsesSendingPreference(t *testing.T) {
	for _, tc := range []struct {
		name, preference, schedule, wantLabel string
	}{
		{name: "direct", preference: "direct", wantLabel: "Send"},
		{name: "scheduled", preference: "scheduled", wantLabel: "Choose sending time"},
		{name: "saved schedule takes precedence", preference: "direct", schedule: intake.ScheduleScheduled, wantLabel: "View schedule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := taskOverview{
				Task:  &intake.Task{ID: "task", SendingType: tc.preference},
				Facts: presentation.Facts{Status: "ready", HasRevision: true, Approved: true, MailboxUsable: true, Eligible: 1, Remaining: 1, ScheduleState: tc.schedule},
			}
			row := campaignTaskRow(&campaignOverview{}, task, nil)
			if row.NextLabel != tc.wantLabel {
				t.Fatalf("next action = %q, want %q", row.NextLabel, tc.wantLabel)
			}
		})
	}
}
