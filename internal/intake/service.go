package intake

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// Service owns the task intake lifecycle: staging an upload, validating it into
// an immutable task snapshot, revalidating an unstarted task against the
// campaign's current revision, serving the private validation artifacts and
// answering the eligibility/reservation contract later dispatch actions use.
//
// Nothing here sends email and no task becomes runnable here: validation
// publishes a snapshot and stops.
type Service struct {
	app       core.App
	campaigns *campaign.Service
	store     *Store
	limits    Limits
	// writeGuard, when set, refuses new sending-data writes at the start of
	// Stage, Validate, Revalidate and Claim. It exists so a mailbox reset can
	// block intake while it deletes disclosed data.
	writeGuard func(ctx context.Context, ownerID string) error
}

// NewService returns the intake service. Limits are normalized once, so a
// zero-valued field means the documented default.
func NewService(app core.App, campaigns *campaign.Service, limits Limits) *Service {
	resolved, err := NormalizeLimits(limits)
	if err != nil {
		// Unusable limits are a startup misconfiguration, not an operator
		// input: falling back to the documented defaults is the only safe
		// reading, and config.Load already rejects a bad configuration.
		resolved = DefaultLimits()
	}
	return &Service{
		app:       app,
		campaigns: campaigns,
		store:     NewStore(app),
		limits:    resolved,
	}
}

// Limits returns the effective upload limits, so the upload screen can show the
// constraints an operator has to work within.
func (s *Service) Limits() Limits { return s.limits }

// SetWriteGuard binds the guard that refuses new sending-data writes while a
// mailbox reset is deleting the operator's disclosed data. The guard error is
// returned unchanged to the caller.
func (s *Service) SetWriteGuard(fn func(ctx context.Context, ownerID string) error) {
	s.writeGuard = fn
}

// guard runs the bound write guard for one operator.
func (s *Service) guard(ctx context.Context, ownerID string) error {
	if s.writeGuard == nil {
		return nil
	}
	return s.writeGuard(ctx, ownerID)
}

// ownedTask loads a task record only when its campaign belongs to the trusted
// operator. A task that does not exist and a task of another operator are the
// same ErrNotFound, so an id can never be probed.
func (s *Service) ownedTask(ctx context.Context, ownerID, taskID string) (*core.Record, error) {
	record, err := s.app.FindFirstRecordByFilter(
		TasksCollection,
		"id = {:id} && campaign.owner = {:owner}",
		dbx.Params{"id": taskID, "owner": ownerID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return record, nil
}

// Preview is the pinned personalized content of one admitted row, rendered from
// the task's pinned revision and the row's immutable raw values. It is the same
// entry point a later dispatch action uses to build the email it submits.
type Preview struct {
	ConfigurationID string
	ContentHash     string
	Approved        bool
	SourceRow       int
	// RecipientIndex is the entry's position inside its source recipient cell.
	RecipientIndex int
	// OriginalRecipient is the full original cell, shown for comparison only.
	OriginalRecipient string
	Rendered          campaign.Rendered
}

// Stage stores one uploaded workbook as a staging task. The workbook is opened
// first: an unsupported, corrupt, oversized or password-protected file is
// rejected here, before any record exists. A staged task is never sendable.
func (s *Service) Stage(ctx context.Context, ownerID, campaignID, filename string, data []byte) (*Task, error) {
	return s.StageWithSendingType(ctx, ownerID, campaignID, filename, data, "")
}

// StageWithSendingType stores the optional first-start preference atomically
// with the staged upload. The preference selects future page state only; it
// does not validate, schedule or start candidate sending.
func (s *Service) StageWithSendingType(ctx context.Context, ownerID, campaignID, filename string, data []byte, sendingType string) (*Task, error) {
	switch sendingType {
	case "", TaskSendingTypeDirect, TaskSendingTypeScheduled:
	default:
		return nil, errors.New("choose Direct or Scheduled sending")
	}
	if err := s.guard(ctx, ownerID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(campaignID) == "" {
		return nil, errors.New("a campaign is required for a task upload")
	}
	if len(data) == 0 {
		return nil, ErrNoUpload
	}
	target, err := s.campaigns.LoadCampaign(ctx, ownerID, campaignID)
	if err != nil {
		return nil, err
	}

	workbook, err := OpenWorkbook(data, s.limits)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := workbook.Close(); err != nil {
			s.app.Logger().Warn("close intake workbook", "error", err)
		}
	}()

	source, err := filesystem.NewFileFromBytes(data, sourceName(filename))
	if err != nil {
		return nil, fmt.Errorf("store the uploaded workbook: %w", err)
	}
	collection, err := s.app.FindCollectionByNameOrId(TasksCollection)
	if err != nil {
		return nil, fmt.Errorf("%s collection is missing; run migrations first: %w", TasksCollection, err)
	}
	record := core.NewRecord(collection)
	record.Set("campaign", campaignID)
	record.Set("status", TaskStaging)
	record.Set("schedule_state", ScheduleNone)
	record.Set("sending_type", sendingType)
	record.Set("source", source)
	record.Set("source_name", sourceName(filename))
	record.Set("source_digest", WorkbookDigest(data))
	record.Set("sheets", workbook.Sheets)
	record.Set("sheet", workbook.Sheet)
	record.Set("header_row", workbook.HeaderRow)
	if err := s.app.Save(record); err != nil {
		return nil, err
	}
	return taskFromRecord(record, target.Name), nil
}

// SetSendingType saves the task's first-start choice without authorizing work.
// The transaction serializes the choice with schedule and execution changes.
func (s *Service) SetSendingType(ctx context.Context, ownerID, taskID, sendingType string) error {
	if sendingType != TaskSendingTypeDirect && sendingType != TaskSendingTypeScheduled {
		return &ValidationError{Message: "choose Direct or Scheduled sending"}
	}
	if err := s.guard(ctx, ownerID); err != nil {
		return err
	}
	return s.app.RunInTransaction(func(txApp core.App) error {
		txService := *s
		txService.app = txApp
		record, err := txService.ownedTask(ctx, ownerID, taskID)
		if err != nil {
			return err
		}
		status := record.GetString("status")
		if record.GetString("closed_at") != "" || record.GetString("closure_requested_at") != "" {
			return &ValidationError{Message: "this task is closed; upload a new task for future work"}
		}
		if (status != TaskStaging && status != TaskReady) || record.GetString("schedule_state") == ScheduleFired {
			return &ValidationError{Message: "sending type cannot be changed after this task has started"}
		}
		if sendingType == TaskSendingTypeDirect && (record.GetString("schedule_state") == ScheduleScheduled || record.GetString("schedule_state") == ScheduleMissed) {
			return &ValidationError{Message: "clear the saved schedule before choosing Direct sending"}
		}
		record.Set("sending_type", sendingType)
		return txApp.Save(record)
	})
}

// LoadTask returns one task of the operator.
func (s *Service) LoadTask(ctx context.Context, ownerID, id string) (*Task, error) {
	record, err := s.ownedTask(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	target, err := s.campaigns.LoadCampaign(ctx, ownerID, record.GetString("campaign"))
	if err != nil {
		return nil, err
	}
	return taskFromRecord(record, target.Name), nil
}

// ListTasks returns a campaign's tasks, newest first.
func (s *Service) ListTasks(ctx context.Context, ownerID, campaignID string) ([]*Task, error) {
	target, err := s.campaigns.LoadCampaign(ctx, ownerID, campaignID)
	if err != nil {
		return nil, err
	}
	records, err := s.app.FindRecordsByFilter(
		TasksCollection,
		"campaign = {:campaign}",
		"-created",
		0,
		0,
		dbx.Params{"campaign": campaignID},
	)
	if err != nil {
		return nil, err
	}
	tasks := make([]*Task, 0, len(records))
	for _, record := range records {
		tasks = append(tasks, taskFromRecord(record, target.Name))
	}
	return tasks, nil
}

// Rows returns one task's admitted membership snapshot, ordered by original
// worksheet row. The task is authorized first, so another operator's rows are
// never listed under a guessed id.
func (s *Service) Rows(ctx context.Context, ownerID, taskID string) ([]RowSnapshot, error) {
	if _, err := s.ownedTask(ctx, ownerID, taskID); err != nil {
		return nil, err
	}
	records, err := s.app.FindRecordsByFilter(
		TaskCandidatesCollection,
		"task = {:task}",
		"source_row,recipient_index",
		0,
		0,
		dbx.Params{"task": taskID},
	)
	if err != nil {
		return nil, err
	}
	rows := make([]RowSnapshot, 0, len(records))
	for _, record := range records {
		row := RowSnapshot{
			ID:                record.Id,
			CandidateID:       record.GetString("candidate"),
			SourceRow:         record.GetInt("source_row"),
			RecipientIndex:    storedPosition(record.GetInt("recipient_index")),
			OriginalRecipient: record.GetString("original_recipient"),
			State:             record.GetString("state"),
			Reason:            record.GetString("reason"),
		}
		if err := record.UnmarshalJSONField("raw", &row.Raw); err != nil {
			row.Raw = nil
		}
		if err := record.UnmarshalJSONField("normalized", &row.Normalized); err != nil {
			row.Normalized = nil
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Manifest returns one task's published validation manifest. It is the
// authoritative map of every candidate row back to its original worksheet row,
// including the rows that were excluded from candidate storage.
func (s *Service) Manifest(ctx context.Context, ownerID, taskID string) (Manifest, error) {
	record, err := s.ownedTask(ctx, ownerID, taskID)
	if err != nil {
		return Manifest{}, err
	}
	data, err := s.readArtifact(ctx, record, "manifest")
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("read the validation manifest: %w", err)
	}
	return manifest, nil
}

// ExclusionReport returns the stored private exclusion report: every row that
// was held back, with its original columns, row number and reason. It exists
// immediately after validation, whatever the eligible count.
func (s *Service) ExclusionReport(ctx context.Context, ownerID, taskID string) ([]byte, string, error) {
	record, err := s.ownedTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, "", err
	}
	if record.GetString("excluded") == "" {
		return nil, "", errors.New("this task has no exclusion report yet")
	}
	data, err := s.readArtifact(ctx, record, "excluded")
	if err != nil {
		return nil, "", err
	}
	return data, "excluded-rows-" + shortID(record.Id) + ".xlsx", nil
}

// Preview renders one admitted row's personalized content from the task's
// pinned revision. The render is a pure function of that revision and the row's
// immutable raw snapshot, so a later campaign edit cannot change it.
func (s *Service) Preview(ctx context.Context, ownerID, taskID, rowID string) (*Preview, error) {
	task, err := s.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if task.ConfigurationID == "" {
		return nil, errors.New("this task has no content revision yet")
	}
	record, err := s.app.FindRecordById(TaskCandidatesCollection, rowID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("task") != task.ID {
		return nil, errors.New("that row belongs to a different task")
	}
	configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID)
	if err != nil {
		return nil, err
	}
	raw := map[string]string{}
	if err := record.UnmarshalJSONField("raw", &raw); err != nil {
		return nil, fmt.Errorf("read the row snapshot: %w", err)
	}
	rendered, err := configuration.Compiled.Render(raw)
	if err != nil {
		return nil, err
	}
	return &Preview{
		ConfigurationID:   configuration.ID,
		ContentHash:       configuration.ContentHash,
		Approved:          configuration.ApprovalCurrent(),
		SourceRow:         record.GetInt("source_row"),
		RecipientIndex:    storedPosition(record.GetInt("recipient_index")),
		OriginalRecipient: record.GetString("original_recipient"),
		Rendered:          rendered,
	}, nil
}

// storedPosition normalizes a membership saved before positions existed. Such a
// row is a single occurrence at position 1.
func storedPosition(position int) int {
	if position < 1 {
		return 1
	}
	return position
}

// Validate turns a staging task into a ready one by validating the selected
// worksheet and header row against the campaign's current content revision.
// Nothing becomes sendable: a ready task holds a published snapshot.
func (s *Service) Validate(ctx context.Context, ownerID, taskID, sheet string, headerRow int) error {
	if err := s.guard(ctx, ownerID); err != nil {
		return err
	}
	task, err := s.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return err
	}
	if task.Closed() || task.Closing() {
		return &ValidationError{Message: "this task is closed; upload a new task for future work"}
	}
	if task.Status != TaskStaging {
		return &ValidationError{Message: "this task already has a validation snapshot; revalidate it instead of choosing a worksheet again"}
	}
	return s.validate(ctx, ownerID, task, sheet, headerRow)
}

// Revalidate reruns validation for an unstarted task against the campaign's
// current content revision, replacing the preview snapshot. A task that has
// already started is never touched: its snapshot is history.
func (s *Service) Revalidate(ctx context.Context, ownerID, taskID string) error {
	if err := s.guard(ctx, ownerID); err != nil {
		return err
	}
	task, err := s.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return err
	}
	if task.Closed() || task.Closing() {
		return &ValidationError{Message: "this task is closed; upload a new task for future work"}
	}
	if task.Status != TaskReady {
		return &ValidationError{Message: "only a validated task that has not started can be revalidated"}
	}
	if task.Sheet == "" {
		return &ValidationError{Message: "this task has no worksheet selection to revalidate"}
	}
	return s.validate(ctx, ownerID, task, task.Sheet, task.HeaderRow)
}

// Preflight answers whether a later start, resume or retry action may proceed,
// with fresh counts and the candidates that are free right now. It reserves
// nothing: a start action claims through Claim after this check.
func (s *Service) Preflight(ctx context.Context, ownerID, taskID string) (*Preflight, error) {
	task, err := s.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	result := &Preflight{
		TaskID:     task.ID,
		CampaignID: task.CampaignID,
		Counts:     task.Counts,
		Ready:      task.Admitted(),
	}
	if !result.Ready {
		result.Reasons = append(result.Reasons, "this task has not been validated yet")
		return result, nil
	}

	target, err := s.campaigns.LoadCampaign(ctx, ownerID, task.CampaignID)
	if err != nil {
		return nil, err
	}
	configuration, err := s.campaigns.CurrentConfiguration(ctx, ownerID, task.CampaignID)
	if err != nil {
		result.Stale = true
		result.Reasons = append(result.Reasons, "the campaign has no current content revision; create one and revalidate this task")
		return result, nil
	}
	result.ConfigurationID = configuration.ID
	result.Approved = configuration.ApprovalCurrent()
	result.ResultLabels = TaskResultLabels(target.ResultLabels, task.Headers)
	result.Stale = configuration.ContentHash != task.ContentHash
	if result.Stale {
		result.Reasons = append(result.Reasons, "the campaign content changed after this task was validated; revalidate this task before starting")
	}
	if !result.Approved {
		result.Reasons = append(result.Reasons, "the campaign's current content revision is not approved yet")
	}

	rows, err := s.Rows(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	claimable, err := s.claimable(rows)
	if err != nil {
		return nil, err
	}
	result.Claimable = claimable
	if len(claimable) == 0 {
		result.Reasons = append(result.Reasons, "no candidate row of this task can be scheduled right now")
	}
	return result, nil
}

// Claim reserves the still-free candidates of one ready task. Only the
// candidates of that task's eligible rows are reservable: every other requested
// id is answered with the state observed for it instead of being reserved.
func (s *Service) Claim(ctx context.Context, ownerID, taskID string, candidateIDs []string) (map[string]ClaimOutcome, error) {
	if err := s.guard(ctx, ownerID); err != nil {
		return nil, err
	}
	task, err := s.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if !task.Admitted() {
		return nil, errors.New("only a validated task can reserve candidates")
	}
	rows, err := s.Rows(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.State == RowEligible {
			eligible[row.CandidateID] = struct{}{}
		}
	}
	allowed := make([]string, 0, len(candidateIDs))
	refused := make([]string, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if _, ok := eligible[id]; ok {
			allowed = append(allowed, id)
		} else {
			refused = append(refused, id)
		}
	}
	outcomes, err := s.store.Claim(ctx, taskID, allowed)
	if err != nil {
		return nil, err
	}
	if outcomes == nil {
		outcomes = map[string]ClaimOutcome{}
	}
	for _, id := range refused {
		// An id that is not an eligible row of this task is never reserved
		// here; the state is reported so a caller can explain the exclusion.
		// An id outside the operator's campaigns has no state to report, so
		// it is answered like any other unresolvable candidate.
		record, err := s.ownedCandidate(ctx, ownerID, id)
		if err != nil {
			outcomes[id] = ClaimOutcome{Claimed: false, State: CandidateClaimed}
			continue
		}
		outcomes[id] = ClaimOutcome{Claimed: false, State: candidateFromRecord(record).State()}
	}
	return outcomes, nil
}

// Release clears the reservations one task owns, so a stopped or recovered task
// releases only safe, unstarted claims. Submission and uncertainty history is
// never cleared, and a later resume rechecks instead of reclaiming blindly.
func (s *Service) Release(ctx context.Context, ownerID, taskID string) (int, error) {
	if strings.TrimSpace(taskID) == "" {
		return 0, errors.New("a task is required to release reservations")
	}
	if _, err := s.ownedTask(ctx, ownerID, taskID); err != nil {
		return 0, err
	}
	return s.store.Release(ctx, taskID)
}

// MarkSubmitted records a confirmed submission for one candidate. It exists for
// the dispatch change that owns sending; intake only defines the history that
// makes a candidate ineligible for every later upload. Ownership is verified
// through the candidate's campaign, so another operator's history is never
// touched.
func (s *Service) MarkSubmitted(ctx context.Context, ownerID, candidateID string) error {
	if _, err := s.ownedCandidate(ctx, ownerID, candidateID); err != nil {
		return err
	}
	return s.store.MarkSubmitted(ctx, candidateID)
}

// ownedCandidate loads a candidate record only when its campaign belongs to the
// trusted operator. A missing candidate and a candidate of another operator are
// the same ErrNotFound.
func (s *Service) ownedCandidate(ctx context.Context, ownerID, candidateID string) (*core.Record, error) {
	record, err := s.app.FindFirstRecordByFilter(
		CandidatesCollection,
		"id = {:id} && campaign.owner = {:owner}",
		dbx.Params{"id": candidateID, "owner": ownerID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return record, nil
}

// validate is the single staged validation path, shared by Validate and
// Revalidate so an unstarted revalidation cannot drift from the original.
//
// Stages: the stored bytes are checked against their digest, the campaign's
// current revision and the mapped headers are checked, duplicate recipient keys
// are classified across the complete input BEFORE any candidate is touched, and
// only then does one transaction publish the snapshot. A failure before that
// transaction leaves the task unready and records the reason.
func (s *Service) validate(ctx context.Context, ownerID string, task *Task, sheet string, headerRow int) error {
	data, err := s.sourceBytes(ctx, task.ID)
	if err != nil {
		return s.fail(task, &ValidationError{Message: "the stored workbook could not be read; upload the workbook again"})
	}
	if WorkbookDigest(data) != task.SourceDigest {
		return s.fail(task, &ValidationError{Message: "the stored workbook no longer matches the digest recorded at upload; upload the workbook again"})
	}

	target, err := s.campaigns.LoadCampaign(ctx, ownerID, task.CampaignID)
	if err != nil {
		return err
	}
	configuration, err := s.campaigns.CurrentConfiguration(ctx, ownerID, task.CampaignID)
	if err != nil {
		return s.fail(task, &ValidationError{Message: "the campaign has no content revision yet; create one before validating a task"})
	}

	workbook, err := OpenWorkbook(data, s.limits)
	if err != nil {
		return s.fail(task, workbookFailure(err))
	}
	defer func() {
		if err := workbook.Close(); err != nil {
			s.app.Logger().Warn("close intake workbook", "error", err)
		}
	}()
	if err := workbook.Select(sheet, headerRow); err != nil {
		return s.fail(task, workbookFailure(err))
	}
	if err := validateHeaders(workbook, configuration.Mappings); err != nil {
		return s.fail(task, err)
	}

	admissions, err := planRows(workbook, configuration.Mappings)
	if err != nil {
		return s.fail(task, &ValidationError{Message: err.Error(), Err: err})
	}
	return s.publish(ctx, publishInput{
		task:          task,
		target:        target,
		configuration: configuration,
		workbook:      workbook,
		admissions:    admissions,
	})
}

// rowInvalid is a planner-only marker: the row's recipient cell is unusable, so
// it never becomes a candidate. It is not a stored membership state.
const rowInvalid = "invalid"

// admission is one expanded recipient entry's planned outcome before candidate
// storage. Siblings from one source row share everything except the recipient
// token, its position and their own copied value map.
type admission struct {
	sourceRow         int
	recipientIndex    int
	recipient         string
	originalRecipient string
	emailKey          string
	// state is "" for a duplicate entry, rowInvalid for an entry without a
	// usable address, and otherwise the state to apply once the candidate
	// identity is known: RowEligible or RowIneligible.
	state      string
	group      int
	raw        map[string]string
	normalized map[string]string
	missing    []string
}

// planRows expands the selected recipient mapping of every data row into
// individual entries and classifies them before any candidate is touched.
//
// Comma splitting is the only expansion. Every token keeps its 1-based position
// and the full original cell stays available for comparison; the other mapped
// values are copied unchanged, and each entry gets its own map so one sibling's
// normalization can never leak into another. Duplicate detection deliberately
// runs over raw trimmed/lowercase recipient keys across the whole expanded
// input, so an entry that is both duplicated and otherwise invalid is still held
// as a duplicate instead of letting its group member through.
func planRows(workbook *Workbook, mappings []campaign.Mapping) ([]admission, error) {
	recipientOffset := -1
	recipientVariable := ""
	offsets := make(map[string]int, len(mappings))
	for _, mapping := range mappings {
		offset, _ := columnOffset(workbook.Headers, mapping.Header)
		offsets[mapping.Variable] = offset
		if mapping.Recipient {
			recipientOffset = offset
			recipientVariable = mapping.Variable
		}
	}

	planned := make([]admission, 0, len(workbook.Rows))
	duplicates := map[string][]int{}
	var order []string
	for _, row := range workbook.Rows {
		raw := make(map[string]string, len(mappings))
		carriesData := false
		for _, mapping := range mappings {
			value := row.Column(offsets[mapping.Variable])
			raw[mapping.Variable] = value
			if strings.TrimSpace(value) != "" {
				carriesData = true
			}
		}
		if !carriesData {
			// A row whose mapped columns are all empty is not a row of the
			// table: it carries no candidate data, so no feedback is due.
			continue
		}
		// The recipient cell is split only here. A wholly empty cell yields one
		// empty token, which stays visible as one invalid entry.
		cell := row.Column(recipientOffset)
		for index, token := range strings.Split(cell, ",") {
			if len(planned) >= MaxRecipientEntries {
				return nil, ErrTooManyRows
			}
			values := maps.Clone(raw)
			if recipientVariable != "" {
				values[recipientVariable] = token
			}
			entry := admission{
				sourceRow:         row.Number,
				recipientIndex:    index + 1,
				recipient:         token,
				originalRecipient: cell,
				emailKey:          RecipientKey(token),
				raw:               values,
			}
			planned = append(planned, entry)
			if entry.emailKey == "" {
				continue
			}
			if _, seen := duplicates[entry.emailKey]; !seen {
				order = append(order, entry.emailKey)
			}
			duplicates[entry.emailKey] = append(duplicates[entry.emailKey], len(planned)-1)
		}
	}

	// Groups are numbered in order of first appearance so the manifest and the
	// report agree on group identity.
	groups := map[string]int{}
	nextGroup := 0
	for _, key := range order {
		members := duplicates[key]
		if len(members) < 2 {
			continue
		}
		nextGroup++
		groups[key] = nextGroup
		for _, index := range members {
			planned[index].state = ""
			planned[index].group = nextGroup
		}
	}

	for i := range planned {
		entry := &planned[i]
		if entry.group > 0 {
			continue // a duplicate entry is excluded whatever else is wrong
		}
		if _, err := campaign.NormalizeRecipient(entry.recipient); err != nil {
			entry.state = rowInvalid
			continue
		}
		entry.normalized, entry.missing = normalizeValues(mappings, entry.raw)
		entry.state = RowEligible
		if len(entry.missing) > 0 {
			entry.state = RowIneligible
		}
	}
	return planned, nil
}

// normalizeValues prepares one row's replacement values exactly the way the
// renderer does, and reports the required variables it is missing.
func normalizeValues(mappings []campaign.Mapping, raw map[string]string) (map[string]string, []string) {
	normalized := make(map[string]string, len(mappings))
	var missing []string
	for _, mapping := range mappings {
		value := campaign.Normalize(mapping.Format, raw[mapping.Variable])
		if value == "" && !mapping.Required {
			value = campaign.Normalize(mapping.Format, mapping.Fallback)
		}
		if value == "" && mapping.Required {
			missing = append(missing, mapping.Variable)
		}
		normalized[mapping.Variable] = value
	}
	return normalized, missing
}

// publishInput carries everything one transaction needs to publish a snapshot.
type publishInput struct {
	task          *Task
	target        *campaign.Campaign
	configuration *campaign.Configuration
	workbook      *Workbook
	admissions    []admission
}

// publish writes the immutable snapshot, the private manifest and the private
// exclusion report and marks the task ready in one transaction, so an
// interrupted publication can never leave a task ready without its artifacts.
//
// Candidate state is read inside that transaction, which is why the manifest and
// the report are built there too: both are pure computation over data already in
// memory, so no I/O ever happens while the write lock is held.
func (s *Service) publish(ctx context.Context, input publishInput) error {
	headers := input.workbook.HeaderLabels()
	rowsByNumber := make(map[int]Row, len(input.workbook.Rows))
	for _, row := range input.workbook.Rows {
		rowsByNumber[row.Number] = row
	}

	manifest := Manifest{
		Version:   ManifestVersion,
		Sheet:     input.workbook.Sheet,
		HeaderRow: input.workbook.HeaderRow,
		Headers:   headers,
	}
	reasons := map[Occurrence]string{}
	sources := make(map[int]struct{}, len(input.workbook.Rows))
	counts := Counts{Total: len(input.admissions)}
	admitted := make([]admission, 0, len(input.admissions))
	for _, entry := range input.admissions {
		sources[entry.sourceRow] = struct{}{}
		occurrence := Occurrence{Row: entry.sourceRow, Position: entry.recipientIndex}
		row := ManifestRow{
			Row:       entry.sourceRow,
			Position:  entry.recipientIndex,
			Recipient: entry.recipient,
			EmailKey:  entry.emailKey,
		}
		switch entry.state {
		case "":
			counts.Duplicate++
			reasons[occurrence] = ReasonText(ReasonDuplicate)
			row.Reasons = []string{ReasonDuplicate}
			row.Group = entry.group
			manifest.Rows = append(manifest.Rows, row)
		case rowInvalid:
			counts.Invalid++
			reasons[occurrence] = ReasonText(ReasonInvalidAddress)
			row.Reasons = []string{ReasonInvalidAddress}
			manifest.Rows = append(manifest.Rows, row)
		default:
			admitted = append(admitted, entry)
		}
	}
	counts.SourceRows = len(sources)
	manifest.SourceRows = counts.SourceRows
	manifest.Groups = duplicateGroups(input.admissions)

	err := s.app.RunInTransaction(func(txApp core.App) error {
		// An enabled future schedule is the task's explicit start
		// authorization for its current snapshot. Publishing a new snapshot
		// while it is enabled would let the old authorization start revised
		// content, so the request is refused here, inside the same
		// transaction that would repin the task. The check is not merely a
		// hidden form: a forged or stale request gets the same refusal.
		current, err := txApp.FindRecordById(TasksCollection, input.task.ID)
		if err != nil {
			return err
		}
		if current.GetString("schedule_state") == ScheduleScheduled {
			return &ValidationError{Message: "clear the scheduled start before revalidating this task; the saved schedule must not authorize revised content"}
		}
		if _, err := txApp.DB().NewQuery("DELETE FROM " + TaskCandidatesCollection + " WHERE task = {:task}").
			Bind(dbx.Params{"task": input.task.ID}).
			Execute(); err != nil {
			return err
		}
		collection, err := txApp.FindCollectionByNameOrId(TaskCandidatesCollection)
		if err != nil {
			return err
		}
		for i := range admitted {
			entry := &admitted[i]
			occurrence := Occurrence{Row: entry.sourceRow, Position: entry.recipientIndex}
			// The identity is resolved for every unique valid address,
			// including an entry that is missing a required value: the identity
			// is campaign-scoped and a corrected upload must reuse it.
			candidate, err := s.store.EnsureCandidate(txApp, input.task.CampaignID, entry.emailKey)
			if err != nil {
				return err
			}
			state := admissionState(candidate, input.task.ID)
			reason := ""
			switch {
			case state != RowEligible:
				reason = ReasonText(state)
			case len(entry.missing) > 0:
				state = RowIneligible
				reason = (&campaign.MissingRequiredError{Variables: entry.missing}).Error()
			}
			record := core.NewRecord(collection)
			record.Set("task", input.task.ID)
			record.Set("candidate", candidate.ID)
			record.Set("source_row", entry.sourceRow)
			record.Set("recipient_index", entry.recipientIndex)
			record.Set("original_recipient", entry.originalRecipient)
			record.Set("state", state)
			record.Set("reason", reason)
			record.Set("raw", entry.raw)
			record.Set("normalized", entry.normalized)
			if err := txApp.Save(record); err != nil {
				return err
			}
			if state != RowEligible {
				reasons[occurrence] = reason
			}
			countRow(&counts, state)
			manifest.Rows = append(manifest.Rows, ManifestRow{
				Row:       entry.sourceRow,
				Position:  entry.recipientIndex,
				Recipient: entry.recipient,
				EmailKey:  entry.emailKey,
				State:     state,
			})
		}
		slices.SortFunc(manifest.Rows, func(a, b ManifestRow) int {
			if a.Row != b.Row {
				return a.Row - b.Row
			}
			return a.Position - b.Position
		})

		report := make([]ReportRow, 0, len(input.admissions))
		for _, entry := range input.admissions {
			if entry.state == RowEligible {
				continue
			}
			occurrence := Occurrence{Row: entry.sourceRow, Position: entry.recipientIndex}
			row := rowsByNumber[entry.sourceRow]
			values := make([]string, 0, len(headers))
			for offset := range headers {
				values = append(values, row.Column(offset))
			}
			report = append(report, ReportRow{
				Row:       entry.sourceRow,
				Position:  entry.recipientIndex,
				Recipient: strings.TrimSpace(entry.recipient),
				Reasons:   []string{reasons[occurrence]},
				Values:    values,
			})
		}

		manifestBytes, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		reportBytes, err := ExclusionReport(headers, report)
		if err != nil {
			return err
		}
		manifestFile, err := filesystem.NewFileFromBytes(manifestBytes, "validation-manifest.json")
		if err != nil {
			return err
		}
		reportFile, err := filesystem.NewFileFromBytes(reportBytes, "excluded-rows.xlsx")
		if err != nil {
			return err
		}

		record, err := txApp.FindRecordById(TasksCollection, input.task.ID)
		if err != nil {
			return err
		}
		record.Set("status", TaskReady)
		record.Set("sheet", input.workbook.Sheet)
		record.Set("header_row", input.workbook.HeaderRow)
		record.Set("headers", headers)
		record.Set("configuration", input.configuration.ID)
		record.Set("content_hash", input.configuration.ContentHash)
		record.Set("result_labels", TaskResultLabels(input.target.ResultLabels, headers))
		record.Set("counts", counts)
		record.Set("manifest", manifestFile)
		record.Set("excluded", reportFile)
		record.Set("validation_error", "")
		return txApp.Save(record)
	})
	if err != nil {
		return fmt.Errorf("publish the validation snapshot: %w", err)
	}
	return nil
}

// admissionState maps a stored candidate to the membership state of a new row.
// Submission history and unresolved uncertainty take precedence over an active
// claim, and a claim held by this task belongs to this task.
func admissionState(candidate *Candidate, taskID string) string {
	switch candidate.State() {
	case CandidateSubmitted:
		return RowAlreadySubmitted
	case CandidateUncertain:
		return RowReviewRequired
	case CandidateClaimed:
		if candidate.ClaimTaskID == taskID {
			return RowEligible
		}
		return RowAlreadyScheduled
	default:
		return RowEligible
	}
}

// countRow accumulates one admitted row state into the counts. Total is set by
// the caller from the complete candidate-row set, so file-only rows count too.
func countRow(counts *Counts, state string) {
	switch state {
	case RowEligible:
		counts.Eligible++
	case RowIneligible:
		counts.Ineligible++
	case RowAlreadySubmitted:
		counts.AlreadySubmitted++
	case RowAlreadyScheduled:
		counts.AlreadyScheduled++
	case RowReviewRequired:
		counts.ReviewRequired++
	}
}

// duplicateGroups summarizes the planned duplicate groups for the manifest.
func duplicateGroups(admissions []admission) []DuplicateGroup {
	groups := map[int]*DuplicateGroup{}
	var ordered []int
	for _, entry := range admissions {
		if entry.group == 0 {
			continue
		}
		group, ok := groups[entry.group]
		if !ok {
			group = &DuplicateGroup{Group: entry.group, EmailKey: entry.emailKey}
			groups[entry.group] = group
			ordered = append(ordered, entry.group)
		}
		group.Occurrences = append(group.Occurrences, Occurrence{Row: entry.sourceRow, Position: entry.recipientIndex})
	}
	result := make([]DuplicateGroup, 0, len(ordered))
	for _, number := range ordered {
		result = append(result, *groups[number])
	}
	return result
}

// claimable returns the candidate ids of a task's eligible rows that are free
// right now. It is the fresh-count answer a start action shows before claiming.
func (s *Service) claimable(rows []RowSnapshot) ([]string, error) {
	claimable := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.State != RowEligible {
			continue
		}
		candidate, err := s.store.LoadCandidate(s.app, row.CandidateID)
		if err != nil {
			continue
		}
		if candidate.State() == CandidateFree {
			claimable = append(claimable, candidate.ID)
		}
	}
	return claimable, nil
}

// fail records a fixed validation failure on the task without changing its
// status: a staging task stays staging, and a validated task stays validated
// (and stale) until a successful revalidation replaces its snapshot.
func (s *Service) fail(task *Task, failure *ValidationError) error {
	record, err := s.app.FindRecordById(TasksCollection, task.ID)
	if err == nil {
		record.Set("validation_error", failure.Message)
		if err := s.app.Save(record); err != nil {
			return err
		}
	}
	return failure
}

// validateHeaders checks the campaign's mapped headers against the selected
// header row. The recipient header and every mapped source header must exist
// exactly once after trimming; a missing or ambiguous header blocks the whole
// task, because no row of it could be mapped unambiguously.
func validateHeaders(workbook *Workbook, mappings []campaign.Mapping) *ValidationError {
	wanted := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		wanted = append(wanted, strings.TrimSpace(mapping.Header))
	}
	for _, label := range wanted {
		switch len(columnOffsets(workbook.Headers, label)) {
		case 0:
			return &ValidationError{Message: fmt.Sprintf("the workbook has no %q column in the selected header row; add the column or choose another header row", label)}
		case 1:
		default:
			return &ValidationError{Message: fmt.Sprintf("the selected header row has more than one %q column; rename the duplicate columns and upload the workbook again", label)}
		}
	}
	return nil
}

// columnOffsets returns every column of a header row whose label equals the
// given label. Labels are compared exactly: the comparison is case sensitive,
// so two columns that differ only in case stay distinguishable.
func columnOffsets(headers []Header, label string) []int {
	var offsets []int
	for _, header := range headers {
		if header.Label == strings.TrimSpace(label) {
			offsets = append(offsets, header.Index)
		}
	}
	return offsets
}

// columnOffset returns the column of a header label and whether it exists
// exactly once.
func columnOffset(headers []Header, label string) (int, bool) {
	offsets := columnOffsets(headers, label)
	if len(offsets) != 1 {
		return -1, false
	}
	return offsets[0], true
}

// workbookFailure turns a workbook rejection into the operator-facing message
// of the task validation. Every message is fixed text owned by this package.
func workbookFailure(err error) *ValidationError {
	for _, sentinel := range []error{
		ErrUploadTooLarge,
		ErrExpandedTooLarge,
		ErrWorksheetTooLarge,
		ErrUnsupportedWorkbook,
		ErrTooManyRows,
		ErrUnknownSheet,
		ErrInvalidHeaderRow,
	} {
		if errors.Is(err, sentinel) {
			return &ValidationError{Message: sentinel.Error(), Err: sentinel}
		}
	}
	return &ValidationError{Message: ErrCorruptWorkbook.Error(), Err: ErrCorruptWorkbook}
}

// sourceBytes reads the stored original workbook of one task.
func (s *Service) sourceBytes(ctx context.Context, taskID string) ([]byte, error) {
	record, err := s.app.FindRecordById(TasksCollection, taskID)
	if err != nil {
		return nil, err
	}
	return s.readArtifact(ctx, record, "source")
}

// readArtifact reads one private file field of a task record. Files are reached
// through the PocketBase filesystem, never through a public URL: every
// collection rule is nil, so only this server-side path can read them.
func (s *Service) readArtifact(ctx context.Context, record *core.Record, field string) (data []byte, err error) {
	name := record.GetString(field)
	if name == "" {
		return nil, fmt.Errorf("task artifact %q is missing", field)
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

// taskFromRecord maps one send_tasks record to the operator-facing task.
func taskFromRecord(record *core.Record, campaignName string) *Task {
	task := &Task{
		ID:                 record.Id,
		CampaignID:         record.GetString("campaign"),
		CampaignName:       campaignName,
		Status:             record.GetString("status"),
		Sheet:              record.GetString("sheet"),
		HeaderRow:          record.GetInt("header_row"),
		ConfigurationID:    record.GetString("configuration"),
		ContentHash:        record.GetString("content_hash"),
		ValidationError:    record.GetString("validation_error"),
		SourceName:         record.GetString("source_name"),
		SourceDigest:       record.GetString("source_digest"),
		UploadedAt:         record.GetDateTime("created").Time().UTC().Truncate(time.Millisecond),
		ScheduleAt:         record.GetDateTime("schedule_at").Time().UTC(),
		ScheduleState:      record.GetString("schedule_state"),
		SendingType:        record.GetString("sending_type"),
		ClosureRequestedAt: record.GetDateTime("closure_requested_at").Time().UTC(),
		ClosedBy:           record.GetString("closure_actor"),
		ClosedAt:           record.GetDateTime("closed_at").Time().UTC(),

		PreparationInstructions:  record.GetString("preparation_instructions"),
		SelectedPreparationRunID: record.GetString("selected_preparation_run"),
	}
	if task.ScheduleState == "" {
		task.ScheduleState = ScheduleNone
	}
	if err := record.UnmarshalJSONField("sheets", &task.Sheets); err != nil {
		task.Sheets = nil
	}
	if err := record.UnmarshalJSONField("headers", &task.Headers); err != nil {
		task.Headers = nil
	}
	if err := record.UnmarshalJSONField("result_labels", &task.ResultLabels); err != nil {
		task.ResultLabels = nil
	}
	if err := record.UnmarshalJSONField("counts", &task.Counts); err != nil {
		task.Counts = Counts{}
	}
	return task
}

// sourceName keeps the uploaded name but forces an XLSX extension, so the
// stored private file never advertises a format the intake cannot read.
func sourceName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	switch {
	case base == "", base == ".", base == "/":
		return "task.xlsx"
	case !strings.HasSuffix(strings.ToLower(base), ".xlsx"):
		return base + ".xlsx"
	default:
		return base
	}
}

// shortID shortens a record id for a download file name.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
