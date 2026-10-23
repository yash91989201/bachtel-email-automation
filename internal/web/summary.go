package web

import (
	"context"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// campaignOverview is one campaign's owner-scoped task metadata: the current
// revision and its mailbox-scoped approval, plus every task's sending facts.
// Loading it never renders, exports, reserves or sends.
type campaignOverview struct {
	Campaign *campaign.Campaign
	// HasRevision and Revision describe the campaign's current revision.
	HasRevision   bool
	Revision      int
	Approved      bool
	Configuration *campaign.Configuration
	Tasks         []taskOverview
	// SummaryError reports that sending metadata could not be loaded. Rows
	// then show "Unavailable; retry" instead of a zero count or success.
	SummaryError string
}

// taskOverview is one task's derived facts for the list and the workspace
// header.
type taskOverview struct {
	Task       *intake.Task
	Facts      presentation.Facts
	Recipients presentation.RecipientSummary
	ResultAsOf time.Time
}

// loadCampaignOverview builds the campaign summary with a bounded number of
// queries: one campaign, one revision, one task list, one batched sending
// metadata query, one artifact query and one mailbox lookup.
func (s *server) loadCampaignOverview(ctx context.Context, ownerID, campaignID string) (*campaignOverview, error) {
	loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, campaignID)
	if err != nil {
		return nil, err
	}
	overview := &campaignOverview{Campaign: loaded}
	if loaded.CurrentRevisionID != "" {
		configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, loaded.CurrentRevisionID)
		if err != nil {
			return nil, err
		}
		overview.HasRevision = true
		overview.Revision = configuration.Revision
		overview.Configuration = configuration
		approved, err := s.sending.ApprovedFor(ctx, ownerID, configuration.ID, configuration.ContentHash)
		if err != nil {
			return nil, err
		}
		overview.Approved = approved
	}
	tasks, err := s.tasks.ListTasks(ctx, ownerID, campaignID)
	if err != nil {
		return nil, err
	}
	summaries := map[string]*sending.TaskSummary{}
	sendingSummaries, err := s.sending.CampaignSummaries(ctx, ownerID, campaignID)
	if err != nil {
		s.app.Logger().Error("load campaign task summaries", "campaign", campaignID, "error", err)
		overview.SummaryError = "Task metadata is unavailable; retry this page."
	} else {
		summaries = sendingSummaries
	}
	artifacts := map[string]exports.Artifact{}
	if overview.HasRevision || len(tasks) > 0 {
		all, err := s.exports.CampaignArtifacts(ctx, ownerID, campaignID)
		if err != nil {
			return nil, err
		}
		for _, artifact := range all {
			if artifact.Kind == exports.KindTaskResults {
				artifacts[artifact.TaskID] = artifact
			}
		}
	}
	_, senderStatus := s.mailboxSummary(ctx, ownerID)
	mailboxUsable := senderStatus == string(mailbox.StatusConnected)
	for _, task := range tasks {
		summary := summaries[task.ID]
		artifact, hasArtifact := artifacts[task.ID]
		facts := presentation.Facts{
			Staging:       task.Status == intake.TaskStaging && !task.Closed() && !task.Closing(),
			Status:        task.Status,
			HasRevision:   overview.HasRevision,
			MailboxUsable: mailboxUsable,
			Closed:        task.Closed(),
			Closing:       task.Closing(),
		}
		facts.ValidationError = task.ValidationError
		facts.ScheduleState = task.ScheduleState
		facts.Stale = task.Status == intake.TaskReady && currentContentChanged(overview, task)
		if hasArtifact {
			facts.ResultState = artifact.State
			facts.ResultReady = artifact.Ready
		}
		recipients := presentation.RecipientSummary{}
		if summary != nil {
			facts.Approved = summary.Approved
			facts.RestoreReview = summary.RestoreReview
			facts.Eligible = summary.Eligible
			facts.Remaining = summary.Remaining
			facts.Submitted = summary.Submitted
			facts.Failed = summary.Failed
			facts.RetryableFailed = summary.RetryableFailed
			facts.Uncertain = summary.Uncertain
			facts.Held = summary.Uncertain
			recipients = presentation.SummarizeRecipients(task.Counts, summary.Submitted, summary.Uncertain, summary.RetryableFailed)
		}
		overview.Tasks = append(overview.Tasks, taskOverview{Task: task, Facts: facts, Recipients: recipients, ResultAsOf: artifact.AsOf})
	}
	return overview, nil
}

// currentContentChanged reports whether an unstarted task's saved snapshot
// differs from the campaign's current revision. A campaign without a current
// revision counts as changed.
func currentContentChanged(overview *campaignOverview, task *intake.Task) bool {
	if !overview.HasRevision {
		return true
	}
	if task.ConfigurationID == "" {
		return true
	}
	// The task's saved hash is authoritative for its snapshot; the campaign
	// revision defines what revalidation would adopt.
	return task.ContentHash != overview.Configuration.ContentHash
}
