package preparation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// ownedTaskRecord loads a task only when its campaign belongs to the operator.
// A task that does not exist and a task of another operator are the same
// ErrNotFound, so an id can never be probed.
func ownedTaskRecord(app core.App, ownerID, taskID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		intake.TasksCollection,
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

// taskRecord loads a task for one operator through the shared helper.
func (s *Service) taskRecord(ctx context.Context, ownerID, taskID string) (*core.Record, error) {
	return ownedTaskRecord(s.app, ownerID, taskID)
}

// runRecord loads a run only when it belongs to the operator and the named
// task. A foreign or mismatched run is reported as ErrNotFound.
func (s *Service) runRecord(ctx context.Context, ownerID, taskID, runID string) (*core.Record, error) {
	record, err := s.app.FindFirstRecordByFilter(
		RunsCollection,
		"id = {:id} && owner = {:owner} && task = {:task}",
		dbx.Params{"id": runID, "owner": ownerID, "task": taskID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return record, nil
}

// runRecords lists one task's runs newest first.
func (s *Service) runRecords(ctx context.Context, ownerID, taskID string) ([]*core.Record, error) {
	return s.app.FindRecordsByFilter(
		RunsCollection,
		"task = {:task} && owner = {:owner}",
		"-created",
		0,
		0,
		dbx.Params{"task": taskID, "owner": ownerID},
	)
}

// activeRunRecord returns the queued or running run of one task, if any.
func activeRunRecord(app core.App, taskID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		RunsCollection,
		"task = {:task} && (state = {:queued} || state = {:running})",
		dbx.Params{"task": taskID, "queued": RunQueued, "running": RunRunning},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return record, nil
}

// jobRecord returns the single job of one run.
func (s *Service) jobRecord(ctx context.Context, runID string) (*core.Record, error) {
	return s.app.FindFirstRecordByFilter(JobsCollection, "run = {:run}", dbx.Params{"run": runID})
}

// readArtifact reads one private file field of a run record. Files are reached
// through the PocketBase filesystem, never through a public URL.
func (s *Service) readArtifact(ctx context.Context, record *core.Record, field string) (data []byte, err error) {
	name := record.GetString(field)
	if name == "" {
		return nil, ErrNoOutput
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

// loadInput reads the captured input artifact of one run.
func (s *Service) loadInput(ctx context.Context, run *core.Record) (Input, error) {
	data, err := s.readArtifact(ctx, run, "input")
	if err != nil {
		return Input{}, fmt.Errorf("read the preparation input: %w", err)
	}
	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		return Input{}, fmt.Errorf("read the preparation input: %w", err)
	}
	return input, nil
}

// loadOutput reads the published output artifact of one run.
func (s *Service) loadOutput(ctx context.Context, run *core.Record) (Output, error) {
	data, err := s.readArtifact(ctx, run, "output")
	if err != nil {
		return Output{}, err
	}
	var output Output
	if err := json.Unmarshal(data, &output); err != nil {
		return Output{}, fmt.Errorf("read the preparation output: %w", err)
	}
	return output, nil
}

// runFromRecord maps one run record. The job counts and selection flag are
// added by the caller that loaded them.
func runFromRecord(record *core.Record) *Run {
	run := &Run{
		ID:                      record.Id,
		OwnerID:                 record.GetString("owner"),
		TaskID:                  record.GetString("task"),
		Kind:                    record.GetString("kind"),
		State:                   record.GetString("state"),
		Error:                   record.GetString("error"),
		Provider:                ai.Provider(record.GetString("provider")),
		APIRoot:                 record.GetString("api_root"),
		APIFormat:               ai.APIFormat(record.GetString("api_format")),
		Model:                   record.GetString("model"),
		ConfigurationGeneration: record.GetInt("configuration_generation"),
		CredentialGeneration:    record.GetInt("credential_generation"),
		ConfigurationID:         record.GetString("configuration"),
		ContentHash:             record.GetString("content_hash"),
		SourceDigest:            record.GetString("source_digest"),
		PromptVersion:           record.GetInt("base_prompt_version"),
		Prompt:                  record.GetString("base_prompt"),
		Instructions:            record.GetString("instructions"),
		InputDigest:             record.GetString("input_digest"),
		OutputDigest:            record.GetString("output_digest"),
		CreatedAt:               record.GetDateTime("created").Time().UTC(),
		StartedAt:               record.GetDateTime("started_at").Time().UTC(),
		FinishedAt:              record.GetDateTime("finished_at").Time().UTC(),
	}
	if err := record.UnmarshalJSONField("options", &run.Options); err != nil {
		run.Options = RunOptions{}
	}
	if err := record.UnmarshalJSONField("counts", &run.Counts); err != nil {
		run.Counts = Counts{}
	}
	return run
}

// withJob adds the durable execution counters of one run.
func withJob(run *Run, job *core.Record) *Run {
	if job == nil {
		return run
	}
	run.BatchTotal = job.GetInt("batch_total")
	run.BatchDone = job.GetInt("batch_done")
	return run
}
