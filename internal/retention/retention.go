// Package retention owns explicit, operator-confirmed deletion of campaign
// history. Nothing here expires automatically, and deleting generated files is
// deliberately a separate action from deleting campaign records.
package retention

import (
	"context"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// ErrCampaignActive means queued or in-flight work still references the
// campaign, so deleting it would strand an accepted submission.
var ErrCampaignActive = errors.New("stop active work and let in-flight requests settle before deleting this campaign")

// Service deletes campaign records and their stored files on explicit request.
type Service struct {
	app core.App
}

// NewService returns a retention service for one application.
func NewService(app core.App) *Service { return &Service{app: app} }

// DeleteCampaign removes a campaign, its revisions and uploaded documents, its
// tasks and retained workbooks, candidate identities, attempts, approvals,
// resolutions and generated downloads. Duplicate-prevention history is part of
// the deletion by design, so a later campaign can send to the same recipients.
//
// Ownership is verified before anything is deleted: an absent campaign and a
// campaign owned by another operator both return campaign.ErrNotFound and
// leave every record untouched.
//
// The delete is idempotent from the operator's perspective: after a partial
// failure the same request can be retried, and it only ever touches records
// explicitly scoped to this campaign.
func (s *Service) DeleteCampaign(ctx context.Context, ownerID, campaignID string) error {
	target, err := s.app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil || target.GetString("owner") != ownerID {
		return campaign.ErrNotFound
	}
	return s.app.RunInTransaction(func(txApp core.App) error {
		tasks, err := find(txApp, intake.TasksCollection, "campaign={:c}", dbx.Params{"c": campaignID})
		if err != nil {
			return err
		}
		for _, task := range tasks {
			switch task.GetString("status") {
			case "running", "waiting", "stopping":
				return ErrCampaignActive
			}
		}
		for _, collection := range []string{intake.TaskCandidatesCollection} {
			rows, err := find(txApp, collection, "task.campaign={:c} && (dispatch_state='reserved' || dispatch_state='in_flight')", dbx.Params{"c": campaignID})
			if err != nil {
				return err
			}
			if len(rows) > 0 {
				return ErrCampaignActive
			}
		}
		attempts, err := find(txApp, sending.AttemptsCollection, "task.campaign={:c} && (state='queued' || state='in_flight')", dbx.Params{"c": campaignID})
		if err != nil {
			return err
		}
		if len(attempts) > 0 {
			return ErrCampaignActive
		}

		for _, spec := range []struct {
			collection string
			filter     string
		}{
			{exports.ArtifactsCollection, "campaign={:c}"},
			{sending.ResolutionsCollection, "member.task.campaign={:c}"},
			{sending.ApprovalsCollection, "attempt.task.campaign={:c}"},
			{sending.ActionsCollection, "task.campaign={:c}"},
			{sending.AttemptsCollection, "task.campaign={:c}"},
			{intake.TaskCandidatesCollection, "task.campaign={:c}"},
			{intake.TasksCollection, "campaign={:c}"},
			{intake.CandidatesCollection, "campaign={:c}"},
			{campaign.ConfigurationsCollection, "campaign={:c}"},
		} {
			rows, err := find(txApp, spec.collection, spec.filter, dbx.Params{"c": campaignID})
			if err != nil {
				return err
			}
			for _, record := range rows {
				if err := txApp.Delete(record); err != nil {
					return fmt.Errorf("delete %s record %s: %w", spec.collection, record.Id, err)
				}
			}
		}
		target, err := txApp.FindRecordById(campaign.CampaignsCollection, campaignID)
		if err != nil {
			return fmt.Errorf("reload campaign for deletion: %w", err)
		}
		if err := txApp.Delete(target); err != nil {
			return fmt.Errorf("delete campaign record: %w", err)
		}
		return nil
	})
}

func find(app core.App, collection, filter string, params dbx.Params) ([]*core.Record, error) {
	return app.FindRecordsByFilter(collection, filter, "", 0, 0, params)
}
