package finalization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// MaxArtifactBytes bounds one stored snapshot artifact. It matches the
// preparation artifact bound, so a legitimate support-bound task fits.
const MaxArtifactBytes = int64(64 << 20)

// Service owns final snapshot creation and its protected downloads.
type Service struct {
	app       core.App
	tasks     *intake.Service
	campaigns *campaign.Service
	prep      *preparation.Service
}

// NewService returns the finalization service.
func NewService(app core.App, tasks *intake.Service, campaigns *campaign.Service, prep *preparation.Service) *Service {
	return &Service{app: app, tasks: tasks, campaigns: campaigns, prep: prep}
}

// ApproveAsIs seals the currently selected completed preparation run without a
// reupload or an AI call. Backend eligibility is recomputed; accepted prepared
// values become the task's final snapshot.
func (s *Service) ApproveAsIs(ctx context.Context, ownerID, taskID, actionID, actor string) (*Snapshot, error) {
	if err := s.actionInput(actionID); err != nil {
		return nil, err
	}
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if err := guardTask(task); err != nil {
		return nil, err
	}
	if task.SelectedPreparationRunID == "" {
		return nil, ErrNoPrepared
	}
	run, err := s.prep.LoadRun(ctx, ownerID, taskID, task.SelectedPreparationRunID)
	if err != nil {
		return nil, err
	}
	if run.State != preparation.RunCompleted || run.Output == nil {
		return nil, ErrNoPrepared
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, err
	}
	if run.ConfigurationID != task.ConfigurationID || run.ContentHash != task.ContentHash || run.SourceDigest != task.SourceDigest {
		return nil, ErrStale
	}
	rows, err := s.preparedRows(ctx, ownerID, taskID, run.Output)
	if err != nil {
		return nil, err
	}
	classification, err := s.classify(ctx, ownerID, configuration, task.ID, rows)
	if err != nil {
		return nil, err
	}
	return s.seal(ctx, sealInput{
		task:           task,
		configuration:  configuration,
		kind:           KindPrepared,
		runID:          run.ID,
		actionID:       actionID,
		actor:          actor,
		sourceDigest:   run.InputDigest,
		classification: classification,
		expectedRunID:  task.SelectedPreparationRunID,
	})
}

// AcceptUpload seals an operator-edited final sheet. The upload receives
// deterministic backend validation only: no AI call, no recasing, no list
// normalization. A structurally invalid upload leaves the selected snapshot
// untouched; a structurally valid upload with zero accepted entries replaces
// the selection with an unsendable snapshot.
func (s *Service) AcceptUpload(ctx context.Context, ownerID, taskID, actionID, actor, filename string, data []byte) (*Snapshot, error) {
	if err := s.actionInput(actionID); err != nil {
		return nil, err
	}
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if err := guardTask(task); err != nil {
		return nil, err
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, err
	}
	rows, err := parseSendingSheet(data, configuration)
	if err != nil {
		s.recordFailure(taskID, err)
		return nil, err
	}
	original, err := s.originalValues(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		if values, ok := original[intake.Occurrence{Row: rows[index].FinalRow, Position: 1}]; ok {
			rows[index].Original = values
		}
	}
	classification, err := s.classify(ctx, ownerID, configuration, task.ID, rows)
	if err != nil {
		s.recordFailure(taskID, err)
		return nil, err
	}
	return s.seal(ctx, sealInput{
		ownerID:        ownerID,
		task:           task,
		configuration:  configuration,
		kind:           KindUpload,
		actionID:       actionID,
		actor:          actor,
		sourceDigest:   digestHex(data),
		uploadName:     filename,
		upload:         data,
		classification: classification,
	})
}

// Selected returns the task's selected final snapshot, or ErrNoSnapshot.
func (s *Service) Selected(ctx context.Context, ownerID, taskID string) (*Snapshot, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if task.SelectedFinalSnapshotID == "" {
		return nil, ErrNoSnapshot
	}
	return s.Load(ctx, ownerID, taskID, task.SelectedFinalSnapshotID)
}

// Load returns one task's snapshot with its entries and exclusions.
func (s *Service) Load(ctx context.Context, ownerID, taskID, snapshotID string) (*Snapshot, error) {
	record, err := s.app.FindFirstRecordByFilter(
		SnapshotsCollection,
		"id = {:id} && owner = {:owner} && task = {:task}",
		dbx.Params{"id": snapshotID, "owner": ownerID, "task": taskID},
	)
	if err != nil {
		return nil, ErrNotFound
	}
	return s.loadRecord(ctx, record)
}

// List returns one task's snapshots newest first.
func (s *Service) List(ctx context.Context, ownerID, taskID string) ([]*Snapshot, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	records, err := s.app.FindRecordsByFilter(
		SnapshotsCollection,
		"task = {:task} && owner = {:owner}",
		"-created",
		0,
		0,
		dbx.Params{"task": taskID, "owner": ownerID},
	)
	if err != nil {
		return nil, err
	}
	snapshots := make([]*Snapshot, 0, len(records))
	for _, record := range records {
		snapshot, err := s.loadRecord(ctx, record)
		if err != nil {
			return nil, err
		}
		snapshot.Selected = record.Id == task.SelectedFinalSnapshotID
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// PreparedSheet builds the prepared sending workbook from the selected
// completed run: accepted mapped values only, one row per recipient entry.
func (s *Service) PreparedSheet(ctx context.Context, ownerID, taskID string) ([]byte, string, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, "", err
	}
	if task.SelectedPreparationRunID == "" {
		return nil, "", ErrNoPrepared
	}
	review, err := s.ReviewPrepared(ctx, ownerID, taskID, task.SelectedPreparationRunID)
	if err != nil {
		return nil, "", err
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, "", err
	}
	rows := sheetRowsFromEntries(review.Entries)
	if len(rows) == 0 {
		return nil, "", errors.New("the selected preparation run has no accepted entries to download")
	}
	data, err := buildSendingSheet(configuration, rows)
	if err != nil {
		return nil, "", err
	}
	return data, "prepared-sending-" + shortID(task.ID) + ".xlsx", nil
}

// FinalSheet returns the stored accepted-final sending workbook.
func (s *Service) FinalSheet(ctx context.Context, ownerID, taskID, snapshotID string) ([]byte, string, error) {
	snapshot, err := s.Load(ctx, ownerID, taskID, snapshotID)
	if err != nil {
		return nil, "", err
	}
	data, err := s.readArtifact(ctx, snapshot.ID, "final_sheet")
	if err != nil {
		return nil, "", err
	}
	return data, "final-sending-" + shortID(taskID) + "-" + shortID(snapshot.ID) + ".xlsx", nil
}

// ExclusionSheet returns the stored exclusion report of one snapshot.
func (s *Service) ExclusionSheet(ctx context.Context, ownerID, taskID, snapshotID string) ([]byte, string, error) {
	snapshot, err := s.Load(ctx, ownerID, taskID, snapshotID)
	if err != nil {
		return nil, "", err
	}
	data, err := s.readArtifact(ctx, snapshot.ID, "exclusions")
	if err != nil {
		return nil, "", err
	}
	return data, "final-exclusions-" + shortID(taskID) + "-" + shortID(snapshot.ID) + ".xlsx", nil
}

// guardTask refuses finalization outside an unstarted, unscheduled, open and
// validated task. The same guard runs again inside the sealing transaction, so
// a stale page is never authority.
func guardTask(task *intake.Task) error {
	switch {
	case task.Closed() || task.Closing():
		return ErrClosed
	case task.Status == intake.TaskStaging:
		return ErrNotReady
	case task.Status != intake.TaskReady:
		return ErrStarted
	case task.ScheduleState == intake.ScheduleScheduled:
		return ErrScheduled
	case task.ScheduleState == intake.ScheduleFired:
		return ErrStarted
	default:
		return nil
	}
}

func (s *Service) configuration(ctx context.Context, ownerID string, task *intake.Task) (*campaign.Configuration, error) {
	if task.ConfigurationID == "" {
		return nil, ErrNotReady
	}
	configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID)
	if err != nil {
		return nil, err
	}
	if configuration.ContentHash != task.ContentHash {
		return nil, ErrStale
	}
	current, err := s.campaigns.LoadCampaign(ctx, ownerID, task.CampaignID)
	if err == nil && current.CurrentRevisionID != task.ConfigurationID {
		return nil, ErrStale
	}
	return configuration, nil
}

func (s *Service) actionInput(actionID string) error {
	if strings.TrimSpace(actionID) == "" || len(actionID) > 128 {
		return errors.New("an action ID is required")
	}
	return nil
}

// sealInput carries one finalization request through validation into the
// sealing transaction.
type sealInput struct {
	ownerID        string
	task           *intake.Task
	configuration  *campaign.Configuration
	kind           string
	runID          string
	actionID       string
	actor          string
	sourceDigest   string
	uploadName     string
	upload         []byte
	expectedRunID  string
	classification classification
}

// seal builds every immutable artifact first, then publishes the snapshot,
// rebuilds the task's sending memberships and selects the snapshot in one short
// transaction. A repeated action returns the same snapshot.
func (s *Service) seal(ctx context.Context, input sealInput) (*Snapshot, error) {
	if existing, err := s.byAction(ctx, input.ownerID, input.task.ID, input.actionID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	entries := make([]Entry, 0, len(input.classification.accepted))
	for _, item := range input.classification.accepted {
		entries = append(entries, item.entry)
	}
	entriesBytes, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("encode the final snapshot entries: %w", err)
	}
	if int64(len(entriesBytes)) > MaxArtifactBytes {
		return nil, &StructuralError{Message: "the final snapshot evidence exceeds the supported artifact size; split the workbook and upload one task at a time"}
	}
	exclusionsBytes, err := json.Marshal(input.classification.exclusions)
	if err != nil {
		return nil, fmt.Errorf("encode the final exclusions: %w", err)
	}
	sheetBytes, err := buildSendingSheet(input.configuration, sheetRowsFromEntries(entries))
	if err != nil {
		return nil, err
	}
	exclusionSheet, err := BuildExclusionReport(input.configuration, input.classification.exclusions)
	if err != nil {
		return nil, err
	}
	digest := snapshotDigest(input.configuration.ContentHash, entries)
	counts := input.classification.counts

	var snapshot *Snapshot
	err = s.app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById(intake.TasksCollection, input.task.ID)
		if err != nil {
			return ErrNotFound
		}
		currentTask := taskFromRecord(current)
		if err := guardTask(currentTask); err != nil {
			return err
		}
		if currentTask.ContentHash != input.task.ContentHash || currentTask.ConfigurationID != input.configuration.ID {
			return ErrStale
		}
		if input.expectedRunID != "" && current.GetString("selected_preparation_run") != input.expectedRunID {
			return ErrStale
		}
		if other, err := snapshotByAction(txApp, input.task.ID, input.actionID); err == nil {
			snapshot = other
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		candidateStore := intake.NewStore(txApp)
		for index := range entries {
			entry := &entries[index]
			candidate, err := candidateStore.EnsureCandidate(txApp, currentTask.CampaignID, entry.Recipient)
			if err != nil {
				return err
			}
			state := candidate.State()
			if state != intake.CandidateFree && (state != intake.CandidateClaimed || candidate.ClaimTaskID != currentTask.ID) {
				return ErrStale
			}
			entry.CandidateID = candidate.ID
		}
		entriesBytes, err = json.Marshal(entries)
		if err != nil {
			return err
		}
		digest = snapshotDigest(input.configuration.ContentHash, entries)
		counts = input.classification.counts

		collection, err := txApp.FindCollectionByNameOrId(SnapshotsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", SnapshotsCollection, err)
		}
		record := core.NewRecord(collection)
		owner, err := ownerOfTask(txApp, currentTask.CampaignID)
		if err != nil {
			return err
		}
		record.Set("owner", owner)
		record.Set("task", currentTask.ID)
		record.Set("campaign", currentTask.CampaignID)
		record.Set("configuration", input.configuration.ID)
		record.Set("content_hash", input.configuration.ContentHash)
		record.Set("kind", input.kind)
		if input.runID != "" {
			record.Set("run", input.runID)
		}
		record.Set("action_id", input.actionID)
		record.Set("actor", input.actor)
		record.Set("source_digest", input.sourceDigest)
		record.Set("digest", digest)
		record.Set("counts", counts)
		if err := setArtifact(record, "entries", entriesBytes, "final-entries.json"); err != nil {
			return err
		}
		if err := setArtifact(record, "final_sheet", sheetBytes, "final-sending.xlsx"); err != nil {
			return err
		}
		if err := setArtifact(record, "exclusions", exclusionSheet, "final-exclusions.xlsx"); err != nil {
			return err
		}
		if err := setArtifact(record, "exclusion_entries", exclusionsBytes, "final-exclusions.json"); err != nil {
			return err
		}
		if len(input.upload) > 0 {
			name := input.uploadName
			if name == "" {
				name = "final-upload.xlsx"
			}
			if err := setArtifact(record, "upload", input.upload, name); err != nil {
				return err
			}
		}
		if err := txApp.Save(record); err != nil {
			return err
		}

		if _, err := txApp.DB().NewQuery("DELETE FROM " + intake.TaskCandidatesCollection + " WHERE task = {:task}").
			Bind(dbx.Params{"task": currentTask.ID}).Execute(); err != nil {
			return err
		}
		memberships, err := txApp.FindCollectionByNameOrId(intake.TaskCandidatesCollection)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			member := core.NewRecord(memberships)
			member.Set("task", currentTask.ID)
			member.Set("candidate", entry.CandidateID)
			member.Set("source_row", entry.FinalRow)
			member.Set("recipient_index", entry.Position)
			member.Set("original_recipient", entry.Recipient)
			member.Set("state", intake.RowEligible)
			member.Set("reason", "")
			member.Set("raw", entry.Values)
			member.Set("normalized", entry.Values)
			if err := txApp.Save(member); err != nil {
				return err
			}
		}

		current.Set("selected_final_snapshot", record.Id)
		current.Set("final_snapshot_digest", digest)
		current.Set("finalized_at", time.Now().UTC())
		current.Set("finalization_error", "")
		current.Set("counts", finalCounts(counts))
		if err := txApp.Save(current); err != nil {
			return err
		}
		snapshot = &Snapshot{
			ID:              record.Id,
			TaskID:          currentTask.ID,
			CampaignID:      currentTask.CampaignID,
			ConfigurationID: input.configuration.ID,
			ContentHash:     input.configuration.ContentHash,
			Kind:            input.kind,
			RunID:           input.runID,
			SourceDigest:    input.sourceDigest,
			Digest:          digest,
			Actor:           input.actor,
			Counts:          counts,
			Entries:         entries,
			Exclusions:      input.classification.exclusions,
			CreatedAt:       record.GetDateTime("created").Time().UTC(),
			Selected:        true,
		}
		return nil
	})
	if err != nil {
		if _, ok := errors.AsType[*StructuralError](err); !ok {
			s.recordFailure(input.task.ID, err)
		}
		return nil, err
	}
	return snapshot, nil
}

// recordFailure stores the latest refused finalization on the task without
// changing the selected snapshot or sending state.
func (s *Service) recordFailure(taskID string, err error) {
	message := ""
	var structural *StructuralError
	switch {
	case errors.As(err, &structural):
		message = structural.Message
	default:
		for _, sentinel := range []error{ErrNoPrepared, ErrStale, ErrScheduled, ErrStarted, ErrClosed, ErrTooManyEntries} {
			if errors.Is(err, sentinel) {
				message = sentinel.Error()
				break
			}
		}
	}
	if message == "" {
		message = "the final sheet could not be accepted; download the current sheet and try again"
	}
	record, findErr := s.app.FindRecordById(intake.TasksCollection, taskID)
	if findErr != nil {
		return
	}
	record.Set("finalization_error", message)
	if saveErr := s.app.Save(record); saveErr != nil {
		s.app.Logger().Error("record finalization failure", "task", taskID, "error", saveErr)
	}
}

func (s *Service) loadRecord(ctx context.Context, record *core.Record) (*Snapshot, error) {
	snapshot := &Snapshot{
		ID:              record.Id,
		TaskID:          record.GetString("task"),
		CampaignID:      record.GetString("campaign"),
		ConfigurationID: record.GetString("configuration"),
		ContentHash:     record.GetString("content_hash"),
		Kind:            record.GetString("kind"),
		RunID:           record.GetString("run"),
		SourceDigest:    record.GetString("source_digest"),
		Digest:          record.GetString("digest"),
		Actor:           record.GetString("actor"),
		CreatedAt:       record.GetDateTime("created").Time().UTC(),
	}
	if err := record.UnmarshalJSONField("counts", &snapshot.Counts); err != nil {
		snapshot.Counts = Counts{}
	}
	entries, err := s.readArtifact(ctx, record.Id, "entries")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(entries, &snapshot.Entries); err != nil {
		return nil, fmt.Errorf("read the final snapshot entries: %w", err)
	}
	exclusions, err := s.readArtifact(ctx, record.Id, "exclusion_entries")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(exclusions, &snapshot.Exclusions); err != nil {
		return nil, fmt.Errorf("read the final snapshot exclusions: %w", err)
	}
	return snapshot, nil
}

// snapshotByAction returns the snapshot created by one action ID. A repeat of
// the same action returns the same snapshot; a different action creates a new
// identity even for identical bytes.
func snapshotByAction(app core.App, taskID, actionID string) (*Snapshot, error) {
	record, err := app.FindFirstRecordByFilter(
		SnapshotsCollection,
		"task = {:task} && action_id = {:action}",
		dbx.Params{"task": taskID, "action": actionID},
	)
	if err != nil {
		return nil, ErrNotFound
	}
	service := &Service{app: app}
	return service.loadRecord(context.Background(), record)
}

// byAction returns the snapshot created by one action ID inside the caller's
// operator scope. A repeat of the same action returns the same snapshot.
func (s *Service) byAction(ctx context.Context, ownerID, taskID, actionID string) (*Snapshot, error) {
	record, err := s.app.FindFirstRecordByFilter(
		SnapshotsCollection,
		"task = {:task} && action_id = {:action} && owner = {:owner}",
		dbx.Params{"task": taskID, "action": actionID, "owner": ownerID},
	)
	if err != nil {
		return nil, ErrNotFound
	}
	return s.loadRecord(ctx, record)
}

func ownerOfTask(app core.App, campaignID string) (string, error) {
	record, err := app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil {
		return "", ErrNotFound
	}
	return record.GetString("owner"), nil
}

func (s *Service) readArtifact(ctx context.Context, recordID, field string) (data []byte, err error) {
	record, err := s.app.FindRecordById(SnapshotsCollection, recordID)
	if err != nil {
		return nil, ErrNotFound
	}
	name := record.GetString(field)
	if name == "" {
		return nil, fmt.Errorf("snapshot artifact %q is missing", field)
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

func setArtifact(record *core.Record, field string, data []byte, name string) error {
	file, err := filesystem.NewFileFromBytes(data, name)
	if err != nil {
		return fmt.Errorf("store the final snapshot artifact: %w", err)
	}
	record.Set(field, file)
	return nil
}

// sheetRowsFromEntries converts accepted final entries to sending-sheet rows.
func sheetRowsFromEntries(entries []Entry) []sheetRow {
	rows := make([]sheetRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, sheetRow{Values: entry.Values})
	}
	return rows
}

func snapshotDigest(contentHash string, entries []Entry) string {
	type canonical struct {
		ContentHash string  `json:"content_hash"`
		Entries     []Entry `json:"entries"`
	}
	payload, err := json.Marshal(canonical{ContentHash: contentHash, Entries: entries})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// finalCounts maps final-snapshot outcomes to the mutually exclusive task
// count categories the existing workspace understands.
func finalCounts(counts Counts) intake.Counts {
	other := max(counts.Excluded-counts.Duplicate-counts.Invalid, 0)
	return intake.Counts{
		Total:      counts.Entries,
		SourceRows: counts.SourceRows,
		Eligible:   counts.Accepted,
		Ineligible: other,
		Duplicate:  counts.Duplicate,
		Invalid:    counts.Invalid,
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// taskFromRecord maps the lifecycle fields finalization rechecks.
func taskFromRecord(record *core.Record) *intake.Task {
	task := &intake.Task{
		ID:                      record.Id,
		CampaignID:              record.GetString("campaign"),
		Status:                  record.GetString("status"),
		ConfigurationID:         record.GetString("configuration"),
		ContentHash:             record.GetString("content_hash"),
		ScheduleState:           record.GetString("schedule_state"),
		ClosureRequestedAt:      record.GetDateTime("closure_requested_at").Time().UTC(),
		ClosedAt:                record.GetDateTime("closed_at").Time().UTC(),
		SelectedFinalSnapshotID: record.GetString("selected_final_snapshot"),
	}
	return task
}
