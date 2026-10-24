package intake

import (
	"context"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// publishPreflight pins the source selection for the reviewed-data workflow.
// Raw values are not eligibility evidence: only the cleaned preparation or an
// operator-authored final upload may be classified and admitted by finalization.
func (s *Service) publishPreflight(ctx context.Context, task *Task, target *campaign.Campaign, configuration *campaign.Configuration, workbook *Workbook) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sourceRows := 0
	for _, row := range workbook.Rows {
		for _, mapping := range configuration.Mappings {
			offset, _ := columnOffset(workbook.Headers, mapping.Header)
			if strings.TrimSpace(row.Column(offset)) != "" {
				sourceRows++
				break
			}
		}
	}
	return s.app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById(TasksCollection, task.ID)
		if err != nil {
			return err
		}
		latest := taskFromRecord(current, "")
		if latest.Status != task.Status || latest.ConfigurationID != task.ConfigurationID {
			return &ValidationError{Message: "the task changed while its headers were being checked; reload it before continuing"}
		}
		if latest.Closed() || latest.Closing() || (latest.Status != TaskStaging && latest.Status != TaskReady) || latest.ScheduleState == ScheduleFired {
			return &ValidationError{Message: "only an open task that has not started can have its headers checked"}
		}
		if latest.ScheduleState == ScheduleScheduled {
			return &ValidationError{Message: "clear the scheduled start before revalidating this task; the saved schedule must not authorize revised content"}
		}
		if _, err := txApp.DB().NewQuery("DELETE FROM " + TaskCandidatesCollection + " WHERE task = {:task}").Bind(dbx.Params{"task": task.ID}).Execute(); err != nil {
			return err
		}
		headers := workbook.HeaderLabels()
		current.Set("status", TaskReady)
		current.Set("sheet", workbook.Sheet)
		current.Set("header_row", workbook.HeaderRow)
		current.Set("headers", headers)
		current.Set("configuration", configuration.ID)
		current.Set("content_hash", configuration.ContentHash)
		current.Set("result_labels", TaskResultLabels(target.ResultLabels, headers))
		current.Set("counts", Counts{SourceRows: sourceRows})
		current.Set("manifest", nil)
		current.Set("excluded", nil)
		current.Set("validation_error", "")
		current.Set("selected_preparation_run", "")
		current.Set("selected_final_snapshot", "")
		current.Set("final_snapshot_digest", "")
		current.Set("finalization_error", "")
		current.Set("finalized_at", "")
		return txApp.Save(current)
	})
}
