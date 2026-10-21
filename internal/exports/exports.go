// Package exports produces protected, durable downloads from recorded task,
// candidate and attempt history. Nothing here sends email or changes
// eligibility: a generation failure can only be retried as a file export.
package exports

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// ArtifactsCollection holds one private generated file per download role.
const ArtifactsCollection = "export_artifacts"

// Artifact kinds.
const (
	KindTaskResults     = "task_results"
	KindCampaignReport  = "campaign_report"
	KindCampaignPackage = "campaign_package"
)

// Artifact generation states.
const (
	// StateGenerating means a generation attempt owns the record. Downloads
	// stay hidden until the attempt publishes a file.
	StateGenerating = "generating"
	// StatePending is durable interrupted work waiting for startup rebuilding.
	StatePending = "pending"
	// StateReady means the stored file is complete and downloadable.
	StateReady = "ready"
	// StateFailed means the last attempt failed; the file stays hidden and the
	// operator can retry.
	StateFailed = "failed"
)

const (
	// MaxArtifactBytes bounds one generated file. The package builder enforces
	// the same ceiling while writing, so a failed build is reported instead of
	// being truncated by storage.
	MaxArtifactBytes = int64(128 << 20)
	// staleGeneration is how long a generating record may sit before a retry
	// may reclaim it. A normal generation finishes in seconds.
	staleGeneration = 10 * time.Minute
)

// ErrNotReady reports a download whose file is not published yet.
var ErrNotReady = errors.New("the export is not ready yet")

// ErrInProgress reports an export generation already owned by another attempt.
var ErrInProgress = errors.New("export generation is already in progress")

// ErrNotTerminal reports a task export requested before the task reached a
// terminal or recovered state.
var ErrNotTerminal = errors.New("result generation requires a completed, stopped or recovered task")

// errOrphaned is the fixed operator-facing failure for a generation that
// discovers its task or campaign was deleted, for example by a mailbox reset.
var errOrphaned = errors.New("the task or campaign no longer exists; this export was not generated")

var errAlreadyReady = errors.New("the export is already ready")

// ownerOfCampaign reads the owning operator of a campaign record.
func ownerOfCampaign(app core.App, campaignID string) (string, error) {
	record, err := app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil {
		return "", err
	}
	return record.GetString("owner"), nil
}

// authorizeCampaign loads a campaign only when it exists and belongs to
// ownerID. An absent id and a foreign id return the same campaign.ErrNotFound,
// so an operator can never probe another operator's campaign ids.
func authorizeCampaign(app core.App, ownerID, campaignID string) (*core.Record, error) {
	record, err := app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil || record.GetString("owner") != ownerID {
		return nil, campaign.ErrNotFound
	}
	return record, nil
}

// authorizeTask loads a task only when its campaign belongs to ownerID. An
// absent task and a foreign task both return intake.ErrNotFound.
func authorizeTask(app core.App, ownerID, taskID string) (*core.Record, error) {
	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		return nil, intake.ErrNotFound
	}
	owner, err := ownerOfCampaign(app, task.GetString("campaign"))
	if err != nil || owner != ownerID {
		return nil, intake.ErrNotFound
	}
	return task, nil
}

// Artifact is the safe operator-facing state of one generated download.
type Artifact struct {
	ID         string
	Kind       string
	TaskID     string
	CampaignID string
	State      string
	Error      string
	AsOf       time.Time
	Ready      bool
}

// Service generates and serves durable downloads. Background generations are
// owner-less by design: they derive the owner from the campaign record and
// never mix operators in one file. Session-facing methods take the trusted
// owner id and refuse foreign records with the fixed campaign/intake
// not-found errors.
type Service struct {
	app        core.App
	now        func() time.Time
	wake       chan struct{}
	generation *sync.RWMutex
}

// NewService returns an export service bound to one PocketBase application.
func NewService(app core.App, _ config.Config) *Service {
	return &Service{
		app:        app,
		now:        time.Now,
		wake:       make(chan struct{}, 1),
		generation: new(sync.RWMutex),
	}
}

// Trigger asks the background loop to look for pending marker work now.
func (s *Service) Trigger() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// BindTaskNotifications registers nonblocking, after-commit activation. Call it
// before sending recovery, and start Run only after that recovery has finished.
func (s *Service) BindTaskNotifications() {
	s.app.OnRecordAfterUpdateSuccess(intake.TasksCollection).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("result_generation_needed") && terminalTask(e.Record) {
			s.Trigger()
		}
		return e.Next()
	})
}

func terminalTask(task *core.Record) bool {
	switch task.GetString("status") {
	case "completed", "stopped", "interrupted":
		return true
	default:
		return false
	}
}

// Run drains durable work once at startup and on notifications. Only discovery
// or recovery persistence errors schedule a timer; healthy idle workers sleep.
func (s *Service) Run(ctx context.Context) {
	recovered := false
	delay := time.Second
	for ctx.Err() == nil {
		var err error
		if !recovered {
			err = s.recoverInterrupted(ctx)
			recovered = err == nil
		}
		if err == nil {
			err = s.generatePending(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.app.Logger().Error("discover export work", "error", err)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.wake:
				timer.Stop()
			case <-timer.C:
			}
			delay = min(delay*2, time.Minute)
		} else {
			delay = time.Second
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
		}
	}
}

// Recovery requeues interrupted work atomically, so a persistence failure
// cannot leave a partially recovered pair. Failed and ready files stay put.
func (s *Service) recoverInterrupted(ctx context.Context) error {
	// A temporary recovery failure can leave the server available to browser
	// actions. Never requeue a claim while one of those actions is still writing.
	s.generation.Lock()
	defer s.generation.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.app.RunInTransaction(func(tx core.App) error {
		records, err := tx.FindRecordsByFilter(ArtifactsCollection, "state={:state}", "", 0, 0, dbx.Params{"state": StateGenerating})
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return err
			}
			if record.GetString("kind") == KindTaskResults {
				task, err := tx.FindRecordById(intake.TasksCollection, record.GetString("task"))
				if errors.Is(err, sql.ErrNoRows) {
					// The task was deleted while its export was generating
					// (mailbox reset). Retire the claim; nothing publishes.
					record.Set("state", StateFailed)
					record.Set("error", errOrphaned.Error())
					record.Set("generation_started", "")
					if err := tx.Save(record); err != nil {
						return err
					}
					continue
				}
				if err != nil {
					return err
				}
				record.Set("state", StatePending)
				record.Set("generation_started", "")
				if err := tx.Save(record); err != nil {
					return err
				}
				task.Set("result_generation_needed", true)
				if err := tx.Save(task); err != nil {
					return err
				}
				continue
			}
			record.Set("state", StatePending)
			record.Set("generation_started", "")
			if err := tx.Save(record); err != nil {
				return err
			}
		}
		return nil
	})
}

// generatePending discovers durable work, but ordinary generation failures
// remain failed until the operator explicitly retries them.
func (s *Service) generatePending(ctx context.Context) error {
	tasks, err := s.records(ctx, intake.TasksCollection,
		"result_generation_needed=true && (status='completed' || status='stopped' || status='interrupted')", "created", nil)
	if err != nil {
		return err
	}
	var discoveryErr error
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		artifact, err := s.findArtifact(ctx, KindTaskResults, task.GetString("campaign"), task.Id)
		if err != nil {
			discoveryErr = errors.Join(discoveryErr, err)
			continue
		}
		if artifact != nil && (artifact.GetString("state") == StateFailed || artifact.GetString("state") == StateGenerating) {
			continue
		}
		if err := s.generateMarkedTask(ctx, task.Id); err != nil && !errors.Is(err, ErrInProgress) {
			// An ordinary failure was persisted by GenerateTask. Discovery errors
			// (including inability to persist that failure) still need a wakeup.
			current, lookupErr := s.findArtifact(ctx, KindTaskResults, task.GetString("campaign"), task.Id)
			if lookupErr != nil {
				discoveryErr = errors.Join(discoveryErr, lookupErr)
			} else if current == nil || current.GetString("state") != StateFailed {
				discoveryErr = errors.Join(discoveryErr, err)
			}
		}
	}
	pending, err := s.records(ctx, ArtifactsCollection, "state={:state} && task=''", "created", dbx.Params{"state": StatePending})
	if err != nil {
		return errors.Join(discoveryErr, err)
	}
	campaigns := make(map[string]bool)
	for _, artifact := range pending {
		campaigns[artifact.GetString("campaign")] = true
	}
	for campaignID := range campaigns {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.generateCampaign(ctx, campaignID, false, true); err != nil && !errors.Is(err, ErrInProgress) {
			// Persisted ordinary failures must not schedule discovery retries.
			for _, kind := range []string{KindCampaignReport, KindCampaignPackage} {
				current, lookupErr := s.findArtifact(ctx, kind, campaignID, "")
				if lookupErr != nil {
					discoveryErr = errors.Join(discoveryErr, lookupErr)
				} else if current != nil && (current.GetString("state") == StatePending || current.GetString("state") == StateGenerating) {
					discoveryErr = errors.Join(discoveryErr, err)
				}
			}
		}
	}
	return discoveryErr
}

// GenerateTask builds and publishes the task-results workbook for one
// operator. force rebuilds an already ready artifact; a non-forced call treats
// an existing ready file as success and clears the durable marker. A foreign
// or absent task returns intake.ErrNotFound before any claim or file write.
func (s *Service) GenerateTask(ctx context.Context, ownerID, taskID string, force bool) error {
	if _, err := authorizeTask(s.app, ownerID, taskID); err != nil {
		return err
	}
	s.generation.RLock()
	defer s.generation.RUnlock()
	return s.generateTask(ctx, taskID, force)
}

// Recheck the marker behind the deletion barrier: discovery may have read it
// just before an operator removed the export and cancelled automatic work.
func (s *Service) generateMarkedTask(ctx context.Context, taskID string) error {
	s.generation.RLock()
	defer s.generation.RUnlock()
	task, err := s.app.FindRecordById(intake.TasksCollection, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		// The task vanished between discovery and generation (mailbox reset).
		// Retire its durable claim so nothing publishes and discovery stops.
		return s.failOrphanedArtifacts(ctx, ArtifactsCollection, "kind={:k} && task={:t}",
			dbx.Params{"k": KindTaskResults, "t": taskID})
	}
	if err != nil {
		return err
	}
	if !task.GetBool("result_generation_needed") {
		return nil
	}
	return s.generateTask(ctx, taskID, false)
}

func (s *Service) generateTask(ctx context.Context, taskID string, force bool) error {
	task, err := s.app.FindRecordById(intake.TasksCollection, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.failOrphanedArtifacts(ctx, ArtifactsCollection, "kind={:k} && task={:t}",
			dbx.Params{"k": KindTaskResults, "t": taskID})
	}
	if err != nil {
		return err
	}
	switch task.GetString("status") {
	case "completed", "stopped", "interrupted":
	default:
		return ErrNotTerminal
	}
	artifact, err := s.claim(ctx, KindTaskResults, task.GetString("campaign"), taskID, force)
	if errors.Is(err, errAlreadyReady) {
		return s.clearMarker(taskID)
	}
	if err != nil {
		return err
	}
	snapshot, err := s.taskSnapshot(ctx, task, artifact)
	if err != nil {
		return s.fail(ctx, artifact.Id, err)
	}
	data, err := TaskWorkbook(snapshot)
	if err != nil {
		return s.fail(ctx, artifact.Id, err)
	}
	if err := s.publish(ctx, artifact.Id, data); err != nil {
		return s.fail(ctx, artifact.Id, err)
	}
	return s.clearMarker(taskID)
}

// GenerateCampaign builds the campaign report and ZIP package from one
// consistent snapshot, so both downloads declare the same as-of time. A
// foreign or absent campaign returns campaign.ErrNotFound before any claim.
func (s *Service) GenerateCampaign(ctx context.Context, ownerID, campaignID string, force bool) error {
	if _, err := authorizeCampaign(s.app, ownerID, campaignID); err != nil {
		return err
	}
	defer s.Trigger() // Revisit any pending sibling after a competing request settles.
	return s.generateCampaign(ctx, campaignID, force, false)
}

func (s *Service) generateCampaign(ctx context.Context, campaignID string, force, pendingOnly bool) error {
	s.generation.RLock()
	defer s.generation.RUnlock()
	target, err := s.app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		// The campaign vanished between discovery and generation (mailbox
		// reset). Retire its durable claims so nothing publishes.
		return s.failOrphanedArtifacts(ctx, ArtifactsCollection, "campaign={:c} && task=''",
			dbx.Params{"c": campaignID})
	}
	if err != nil {
		return err
	}
	// Both roles are built from the same snapshot and declare the same time,
	// even if their durable claims fall on opposite sides of a clock tick.
	var report, pkg *core.Record
	err = s.app.RunInTransaction(func(tx core.App) error {
		claimService := *s
		claimService.app = tx
		claimedAt := s.now().UTC()
		claimService.now = func() time.Time { return claimedAt }
		var claimErr error
		report, claimErr = claimService.claimCampaign(ctx, KindCampaignReport, target.Id, force, pendingOnly)
		if errors.Is(claimErr, errAlreadyReady) {
			report = nil
		} else if claimErr != nil {
			return claimErr
		}
		pkg, claimErr = claimService.claimCampaign(ctx, KindCampaignPackage, target.Id, force, pendingOnly)
		if errors.Is(claimErr, errAlreadyReady) {
			pkg = nil
		} else if claimErr != nil {
			return claimErr
		}
		return nil
	})
	if err != nil {
		return err
	}
	if report == nil && pkg == nil {
		return nil
	}
	asOf := s.now().UTC()
	if report != nil {
		asOf = report.GetDateTime("as_of").Time().UTC()
	} else if pkg != nil {
		asOf = pkg.GetDateTime("as_of").Time().UTC()
	}
	snapshot, err := s.campaignSnapshot(ctx, target, asOf)
	if err != nil {
		s.failClaims(ctx, report, pkg, err)
		return err
	}
	reportBytes, err := CampaignReport(snapshot)
	if err != nil {
		s.failClaims(ctx, report, pkg, err)
		return err
	}
	if report != nil {
		if err := s.publish(ctx, report.Id, reportBytes); err != nil {
			s.failClaims(ctx, nil, pkg, err)
			return s.fail(ctx, report.Id, err)
		}
	}
	if pkg != nil {
		packageBytes, err := CampaignPackage(snapshot, reportBytes)
		if err != nil {
			return s.fail(ctx, pkg.Id, err)
		}
		if err := s.publish(ctx, pkg.Id, packageBytes); err != nil {
			return s.fail(ctx, pkg.Id, err)
		}
	}
	return nil
}

func (s *Service) failClaims(ctx context.Context, report, pkg *core.Record, cause error) {
	for _, artifact := range []*core.Record{report, pkg} {
		if artifact == nil {
			continue
		}
		if err := s.fail(ctx, artifact.Id, cause); err != nil {
			s.app.Logger().Error("record export failure", "artifact", artifact.Id, "error", err)
		}
	}
}

// TaskArtifact returns the current state of a task's results download. A
// foreign or absent task returns intake.ErrNotFound.
func (s *Service) TaskArtifact(ctx context.Context, ownerID, taskID string) (*Artifact, error) {
	task, err := authorizeTask(s.app, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	record, err := s.findArtifact(ctx, KindTaskResults, task.GetString("campaign"), taskID)
	if err != nil || record == nil {
		return nil, err
	}
	return artifactFrom(record), nil
}

// CampaignArtifacts returns the report and package state for one campaign. A
// foreign or absent campaign returns campaign.ErrNotFound.
func (s *Service) CampaignArtifacts(ctx context.Context, ownerID, campaignID string) ([]Artifact, error) {
	if _, err := authorizeCampaign(s.app, ownerID, campaignID); err != nil {
		return nil, err
	}
	records, err := s.records(ctx, ArtifactsCollection, "campaign={:c}", "kind", dbx.Params{"c": campaignID})
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, len(records))
	for _, record := range records {
		artifacts = append(artifacts, *artifactFrom(record))
	}
	return artifacts, nil
}

// download returns one ready artifact's bytes and display file name.
func (s *Service) download(ctx context.Context, artifactID string) ([]byte, string, error) {
	record, err := s.app.FindRecordById(ArtifactsCollection, artifactID)
	if err != nil {
		return nil, "", err
	}
	if record.GetString("state") != StateReady {
		return nil, "", ErrNotReady
	}
	data, err := s.readField(ctx, record, "file")
	if err != nil {
		return nil, "", err
	}
	return data, downloadName(record), nil
}

// DownloadTask returns a ready task-results workbook. A foreign or absent
// task returns intake.ErrNotFound without touching the stored file.
func (s *Service) DownloadTask(ctx context.Context, ownerID, taskID string) ([]byte, string, error) {
	artifact, err := s.TaskArtifact(ctx, ownerID, taskID)
	if err != nil {
		return nil, "", err
	}
	if artifact == nil || !artifact.Ready {
		return nil, "", ErrNotReady
	}
	return s.download(ctx, artifact.ID)
}

// DownloadCampaign returns one ready campaign report or package. A foreign or
// absent campaign returns campaign.ErrNotFound.
func (s *Service) DownloadCampaign(ctx context.Context, ownerID, campaignID, kind string) ([]byte, string, error) {
	if _, err := authorizeCampaign(s.app, ownerID, campaignID); err != nil {
		return nil, "", err
	}
	record, err := s.findArtifact(ctx, kind, campaignID, "")
	if err != nil {
		return nil, "", err
	}
	if record == nil || record.GetString("state") != StateReady {
		return nil, "", ErrNotReady
	}
	return s.download(ctx, record.Id)
}

// DeleteTask removes only the generated workbook. Candidate eligibility and
// attempt history are untouched, and a later generation can rebuild it. A
// foreign or absent task returns intake.ErrNotFound with no deletion.
func (s *Service) DeleteTask(ctx context.Context, ownerID, taskID string) error {
	s.generation.Lock()
	defer s.generation.Unlock()
	return s.app.RunInTransaction(func(tx core.App) error {
		task, err := authorizeTask(tx, ownerID, taskID)
		if err != nil {
			return err
		}
		service := *s
		service.app = tx
		if err := service.deleteArtifact(ctx, KindTaskResults, task.GetString("campaign"), taskID); err != nil {
			return err
		}
		task.Set("result_generation_needed", false)
		return tx.Save(task)
	})
}

// DeleteCampaign removes both generated campaign downloads. A foreign or
// absent campaign returns campaign.ErrNotFound with no deletion.
func (s *Service) DeleteCampaign(ctx context.Context, ownerID, campaignID string) error {
	if _, err := authorizeCampaign(s.app, ownerID, campaignID); err != nil {
		return err
	}
	for _, kind := range []string{KindCampaignReport, KindCampaignPackage} {
		if err := s.deleteArtifact(ctx, kind, campaignID, ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteArtifact(ctx context.Context, kind, campaignID, taskID string) error {
	record, err := s.findArtifact(ctx, kind, campaignID, taskID)
	if err != nil || record == nil {
		return err
	}
	if record.GetString("state") == StateGenerating && s.now().Sub(record.GetDateTime("generation_started").Time()) < staleGeneration {
		return ErrInProgress
	}
	return s.app.Delete(record)
}

// AttemptHTML returns one recorded attempt as a static, inert HTML document.
// It uses the stored subject, body and sender only. A foreign or absent
// attempt returns intake.ErrNotFound.
func (s *Service) AttemptHTML(ctx context.Context, ownerID, attemptID string) ([]byte, string, error) {
	record, err := s.app.FindRecordById(sending.AttemptsCollection, attemptID)
	if err != nil {
		return nil, "", intake.ErrNotFound
	}
	if _, err := authorizeTask(s.app, ownerID, record.GetString("task")); err != nil {
		return nil, "", err
	}
	data, err := attemptHTML(record)
	if err != nil {
		return nil, "", err
	}
	return data, "attempt-" + shortID(record.Id) + ".html", nil
}

// AttemptBelongsTo reports whether an attempt is part of a campaign owned by
// operator ownerID, so the download route can enforce the same ownership
// boundary as the page. A foreign attempt reports false without leaking
// whether it exists.
func (s *Service) AttemptBelongsTo(ctx context.Context, ownerID, attemptID, campaignID string) (bool, error) {
	record, err := s.app.FindRecordById(sending.AttemptsCollection, attemptID)
	if err != nil {
		return false, intake.ErrNotFound
	}
	task, err := s.app.FindRecordById(intake.TasksCollection, record.GetString("task"))
	if err != nil {
		return false, intake.ErrNotFound
	}
	owner, err := ownerOfCampaign(s.app, task.GetString("campaign"))
	if err != nil {
		return false, err
	}
	return owner == ownerID && task.GetString("campaign") == campaignID, nil
}

// claimCampaign can restrict startup recovery to previously requested pending
// roles. The check and claim share the same transaction.
func (s *Service) claimCampaign(ctx context.Context, kind, campaignID string, force, pendingOnly bool) (*core.Record, error) {
	if !pendingOnly {
		return s.claim(ctx, kind, campaignID, "", force)
	}
	record, err := findArtifact(s.app, kind, campaignID, "")
	if err != nil {
		return nil, err
	}
	if record != nil && record.GetString("state") == StateGenerating {
		return nil, ErrInProgress
	}
	if record == nil || record.GetString("state") != StatePending {
		return nil, errAlreadyReady
	}
	return s.claim(ctx, kind, campaignID, "", false)
}

func (s *Service) claim(ctx context.Context, kind, campaignID, taskID string, force bool) (*core.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var claimed *core.Record
	err := s.app.RunInTransaction(func(txApp core.App) error {
		record, err := findArtifact(txApp, kind, campaignID, taskID)
		if err != nil {
			return err
		}
		if record == nil {
			collection, err := txApp.FindCollectionByNameOrId(ArtifactsCollection)
			if err != nil {
				return err
			}
			owner, err := ownerOfCampaign(txApp, campaignID)
			if err != nil {
				// A deleted campaign can never mint a new download.
				return errOrphaned
			}
			record = core.NewRecord(collection)
			record.Set("owner", owner)
			record.Set("kind", kind)
			record.Set("campaign", campaignID)
			if taskID != "" {
				record.Set("task", taskID)
			}
			record.Set("state", StateGenerating)
		} else {
			switch record.GetString("state") {
			case StateReady:
				if !force {
					return errAlreadyReady
				}
			case StateGenerating:
				started := record.GetDateTime("generation_started").Time()
				if s.now().Sub(started) < staleGeneration {
					return ErrInProgress
				}
			}
		}
		now := s.now().UTC()
		record.Set("state", StateGenerating)
		record.Set("error", "")
		record.Set("as_of", now)
		record.Set("generation_started", now)
		if err = txApp.Save(record); err != nil {
			return err
		}
		claimed = record
		return nil
	})
	return claimed, err
}

func (s *Service) publish(ctx context.Context, artifactID string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(data)) > MaxArtifactBytes {
		return errors.New("generated file exceeds the artifact size bound")
	}
	record, err := s.app.FindRecordById(ArtifactsCollection, artifactID)
	if err != nil {
		return err
	}
	file, err := filesystem.NewFileFromBytes(data, downloadName(record))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	record.Set("file", file)
	record.Set("state", StateReady)
	record.Set("digest", hex.EncodeToString(sum[:]))
	record.Set("error", "")
	record.Set("generation_started", "")
	if err := s.app.Save(record); err != nil {
		return err
	}
	// The marker is advisory; the artifact state is authoritative. Clearing
	// it after publishing keeps a restart from regenerating a ready file.
	return s.clearMarker(record.GetString("task"))
}

func (s *Service) clearMarker(taskID string) error {
	if taskID == "" {
		return nil
	}
	task, err := s.app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		return err
	}
	if !task.GetBool("result_generation_needed") {
		return nil
	}
	task.Set("result_generation_needed", false)
	return s.app.Save(task)
}

// fail records a retryable failure with fixed operator-facing text.
func (s *Service) fail(ctx context.Context, artifactID string, cause error) error {
	if ctx.Err() != nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		// Keep the durable claim for startup recovery; cancellation is not an
		// ordinary file-generation failure requiring operator intervention.
		return errors.Join(cause, ctx.Err())
	}
	s.app.Logger().Error("export generation failed", "artifact", artifactID, "error", cause)
	record, err := s.app.FindRecordById(ArtifactsCollection, artifactID)
	if err != nil {
		return err
	}
	record.Set("state", StateFailed)
	record.Set("error", "the export could not be generated; retry")
	record.Set("generation_started", "")
	if err = s.app.Save(record); err != nil {
		return err
	}
	return cause
}

// failOrphanedArtifacts retires every unfinished artifact matching filter as
// failed with fixed operator-facing text. It runs when a generation or
// recovery discovers its task or campaign was deleted (mailbox reset), so a
// late generation publishes no file and discovery stops retrying it. A failed
// artifact is never downloadable and an explicit retry re-checks the record.
func (s *Service) failOrphanedArtifacts(ctx context.Context, collection, filter string, params dbx.Params) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	records, err := s.records(ctx, collection, filter, "created", params)
	if err != nil {
		return err
	}
	for _, record := range records {
		switch record.GetString("state") {
		case StatePending, StateGenerating:
		default:
			// Ready files stay downloadable until the operator deletes them;
			// ordinary failures keep their retryable text.
			continue
		}
		record.Set("state", StateFailed)
		record.Set("error", errOrphaned.Error())
		record.Set("generation_started", "")
		if err := s.app.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func findArtifact(app core.App, kind, campaignID, taskID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(ArtifactsCollection,
		"kind={:kind} && campaign={:campaign} && task={:task}",
		dbx.Params{"kind": kind, "campaign": campaignID, "task": taskID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func (s *Service) findArtifact(ctx context.Context, kind, campaignID, taskID string) (*core.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return findArtifact(s.app, kind, campaignID, taskID)
}

func (s *Service) records(ctx context.Context, collection, filter, sort string, params dbx.Params) ([]*core.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.app.FindRecordsByFilter(collection, filter, sort, 0, 0, params)
}

// readField reads one private PocketBase file field through the filesystem.
func (s *Service) readField(ctx context.Context, record *core.Record, field string) (data []byte, err error) {
	name := record.GetString(field)
	if name == "" {
		return nil, errors.New("the export file is missing")
	}
	fsys, err := s.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, fsys.Close()) }()
	fsys.SetContext(ctx)
	reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + name)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	return io.ReadAll(reader)
}

func artifactFrom(record *core.Record) *Artifact {
	asOf := record.GetDateTime("as_of").Time().UTC()
	return &Artifact{
		ID:         record.Id,
		Kind:       record.GetString("kind"),
		TaskID:     record.GetString("task"),
		CampaignID: record.GetString("campaign"),
		State:      record.GetString("state"),
		Error:      record.GetString("error"),
		AsOf:       asOf,
		Ready:      record.GetString("state") == StateReady && record.GetString("file") != "",
	}
}

// downloadName is a stable operator-facing file name derived from the record
// ids, never from uploaded or candidate text.
func downloadName(record *core.Record) string {
	switch record.GetString("kind") {
	case KindTaskResults:
		return "task-results-" + shortID(record.GetString("task")) + ".xlsx"
	case KindCampaignReport:
		return "campaign-report-" + shortID(record.GetString("campaign")) + ".xlsx"
	case KindCampaignPackage:
		return "campaign-package-" + shortID(record.GetString("campaign")) + ".zip"
	default:
		return "export-" + shortID(record.Id)
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
