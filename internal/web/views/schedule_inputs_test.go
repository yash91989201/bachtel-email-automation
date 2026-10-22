package views

import (
	"testing"
)

func TestScheduleInputsUseNativeDateAndTimeControls(t *testing.T) {
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", SendingType: TaskSendingTypeScheduled, Section: TaskSectionSending,
		ActionID: "schedule-key-", Remaining: 1, Approved: true, Controls: TaskControls{Schedule: true},
		ScheduleDraftDate: "2026-10-22", ScheduleDraftTime: "09:05",
	}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/tasks/task-1/sending/schedule#sending")
	if form == nil {
		t.Fatal("scheduled sending form is missing")
	}
	for _, tc := range []struct {
		id, name, inputType, value string
	}{
		{id: "sending-schedule-date", name: "schedule_date", inputType: "date", value: "2026-10-22"},
		{id: "sending-schedule-time", name: "schedule_time", inputType: "time", value: "09:05"},
	} {
		control := workspaceNode(form, "id", tc.id)
		if control == nil || control.Data != "input" {
			t.Errorf("missing %s input", tc.id)
			continue
		}
		if workspaceAttr(control, "name") != tc.name || workspaceAttr(control, "type") != tc.inputType || workspaceAttr(control, "value") != tc.value {
			t.Errorf("%s attributes = name %q type %q value %q", tc.id, workspaceAttr(control, "name"), workspaceAttr(control, "type"), workspaceAttr(control, "value"))
		}
		if !hasViewAttr(control, "required") {
			t.Errorf("%s input must be required", tc.id)
		}
	}
	clock := workspaceNode(form, "id", "sending-schedule-time")
	if got := workspaceAttr(clock, "step"); got != "60" {
		t.Errorf("schedule time step = %q, want 60 seconds", got)
	}
	for _, oldName := range []string{"schedule_hour", "schedule_minute", "schedule_meridiem"} {
		if workspaceNode(form, "name", oldName) != nil {
			t.Errorf("obsolete schedule control %q is still present", oldName)
		}
	}
}
