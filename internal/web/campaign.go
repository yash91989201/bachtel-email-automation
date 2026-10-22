package web

// Campaign routes. Every handler runs behind requireOperator, every failure
// that the operator can fix is re-rendered inline with status 400, and no
// handler here sends email: the preview renders one in-memory sample row and
// writes nothing.

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/retention"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// registerCampaignRoutes mounts the campaign UI on the shared mux. Every
// route runs behind requireOperator.
func (s *server) registerCampaignRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /campaigns", s.requireOperator(s.handleCampaignList))
	mux.HandleFunc("GET /campaigns/new", s.requireOperator(s.handleCampaignNew))
	mux.HandleFunc("POST /campaigns", s.requireOperator(s.handleCampaignCreate))
	mux.HandleFunc("GET /campaigns/{id}", s.requireOperator(s.handleCampaignDetail))
	mux.HandleFunc("POST /campaigns/{id}/revisions", s.requireOperator(s.handleCampaignRevision))
	mux.HandleFunc("POST /campaigns/{id}/labels", s.requireOperator(s.handleCampaignLabels))
	mux.HandleFunc("POST /campaigns/{id}/preview", s.requireOperator(s.handleCampaignPreview))
	mux.HandleFunc("POST /campaigns/{id}/exports", s.requireOperator(s.handleCampaignExportsGenerate))
	mux.HandleFunc("POST /campaigns/{id}/exports/delete", s.requireOperator(s.handleCampaignExportsDelete))
	mux.HandleFunc("GET /campaigns/{id}/exports/report.xlsx", s.requireOperator(s.handleCampaignReportDownload))
	mux.HandleFunc("GET /campaigns/{id}/exports/package.zip", s.requireOperator(s.handleCampaignPackageDownload))
	mux.HandleFunc("GET /campaigns/{id}/attempts/{attempt}/email.html", s.requireOperator(s.handleAttemptHTML))
	mux.HandleFunc("POST /campaigns/{id}/delete", s.requireOperator(s.handleCampaignDelete))
	// No candidate-send surface exists: GET /campaigns/{id} would otherwise
	// make the mux answer 405 (path match, method mismatch) for this path, so
	// the not-found answer is pinned explicitly.
	mux.HandleFunc("POST /campaigns/send", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}

func (s *server) handleCampaignList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	data := views.CampaignListData{}
	campaigns, err := s.campaigns.ListCampaigns(ctx, ownerID)
	if err != nil {
		s.app.Logger().Error("list campaigns", "error", err)
		http.Error(w, "Unable to load campaigns", http.StatusInternalServerError)
		return
	}
	for _, c := range campaigns {
		row := views.CampaignListRow{ID: c.ID, Name: c.Name, Revision: c.Revision, HasRevision: c.CurrentRevisionID != ""}
		overview, err := s.loadCampaignOverview(ctx, ownerID, c.ID)
		if err != nil {
			s.app.Logger().Error("load campaign summary", "campaign", c.ID, "error", err)
			row.AttentionError = "Unavailable; retry"
			data.Campaigns = append(data.Campaigns, row)
			continue
		}
		row.Approved = overview.Approved
		row.TaskCount = len(overview.Tasks)
		if overview.SummaryError != "" {
			row.AttentionError = "Unavailable; retry"
		} else {
			for _, task := range overview.Tasks {
				if presentation.NeedsAttention(task.Facts) {
					row.AttentionCount++
				}
			}
		}
		data.Campaigns = append(data.Campaigns, row)
	}
	// A broken converter blocks new revisions: say so before the operator
	// fills in a whole upload form.
	if err := s.campaigns.ConverterStatus(ctx); err != nil {
		data.ConverterError = converterMessage(err)
	}
	s.renderWithStatus(w, r, http.StatusOK, views.CampaignList(data))
}

func (s *server) handleCampaignNew(w http.ResponseWriter, r *http.Request) {
	data := views.CampaignFormData{ResultLabels: strings.Join(campaign.DefaultResultLabels, ", ")}
	s.renderWithStatus(w, r, http.StatusOK, views.CampaignForm(data))
}

func (s *server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	input := campaign.CreateCampaignInput{
		Name:          r.FormValue("name"),
		SourceHeaders: splitLabels(r.FormValue("source_headers")),
		ResultLabels:  splitLabels(r.FormValue("result_labels")),
	}
	// Validate with the campaign package before touching storage: its
	// messages are fixed strings safe to show inline, while the service
	// repeats the same checks and then only fails on storage problems.
	if message := campaignFormError(input); message != "" {
		data := views.CampaignFormData{
			Error:         message,
			Name:          input.Name,
			SourceHeaders: strings.Join(input.SourceHeaders, ", "),
			ResultLabels:  r.FormValue("result_labels"),
		}
		s.renderWithStatus(w, r, http.StatusBadRequest, views.CampaignForm(data))
		return
	}
	created, err := s.campaigns.CreateCampaign(r.Context(), recordFromContext(r).Id, input)
	if err != nil {
		s.app.Logger().Error("create campaign", "error", err)
		http.Error(w, "Unable to create campaign", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/campaigns/"+created.ID+"#template", http.StatusSeeOther)
}

func (s *server) handleCampaignDetail(w http.ResponseWriter, r *http.Request) {
	data, err := s.campaignPageData(r, r.PathValue("id"), "", r.URL.Query()["warning"])
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.CampaignPage(data))
}

// handleCampaignExportsGenerate rebuilds the campaign report and ZIP from one
// consistent snapshot. Export generation never touches email state.
func (s *server) handleCampaignExportsGenerate(w http.ResponseWriter, r *http.Request) {
	page, err := s.campaignPageData(r, r.PathValue("id"), "", nil)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	if err = s.exports.GenerateCampaign(r.Context(), recordFromContext(r).Id, page.ID, true); err != nil {
		if errors.Is(err, exports.ErrInProgress) {
			page.ExportError = "export generation is already in progress; reload this page shortly"
			s.renderWithStatus(w, r, http.StatusConflict, views.CampaignPage(page))
			return
		}
		s.app.Logger().Error("generate campaign exports", "campaign", page.ID, "error", err)
		page.ExportError = "the campaign downloads could not be generated; retry"
		s.renderWithStatus(w, r, http.StatusInternalServerError, views.CampaignPage(page))
		return
	}
	http.Redirect(w, r, "/campaigns/"+page.ID+"#activity", http.StatusSeeOther)
}

// handleCampaignExportsDelete removes only the generated report and package.
// Campaign records, attempts and candidate eligibility stay unchanged.
func (s *server) handleCampaignExportsDelete(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	if err := s.exports.DeleteCampaign(r.Context(), recordFromContext(r).Id, target); err != nil {
		page, dataErr := s.campaignPageData(r, target, "", nil)
		if dataErr != nil {
			s.campaignLoadFailure(w, r, dataErr)
			return
		}
		s.app.Logger().Error("delete campaign exports", "campaign", target, "error", err)
		page.ExportError = "the generated downloads could not be removed; retry"
		s.renderWithStatus(w, r, http.StatusInternalServerError, views.CampaignPage(page))
		return
	}
	http.Redirect(w, r, "/campaigns/"+target+"#activity", http.StatusSeeOther)
}

func (s *server) handleCampaignReportDownload(w http.ResponseWriter, r *http.Request) {
	data, filename, err := s.exports.DownloadCampaign(r.Context(), recordFromContext(r).Id, r.PathValue("id"), exports.KindCampaignReport)
	if err != nil {
		s.writeDownloadFailure(w, "load campaign report", err)
		return
	}
	s.writeDownload(w, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", filename, data)
}

func (s *server) handleCampaignPackageDownload(w http.ResponseWriter, r *http.Request) {
	data, filename, err := s.exports.DownloadCampaign(r.Context(), recordFromContext(r).Id, r.PathValue("id"), exports.KindCampaignPackage)
	if err != nil {
		s.writeDownloadFailure(w, "load campaign package", err)
		return
	}
	s.writeDownload(w, "application/zip", filename, data)
}

// handleAttemptHTML serves one recorded attempt as a static, isolated HTML
// document. The document declares its own restrictive policy so stored content
// cannot run scripts even if opened directly.
func (s *server) handleAttemptHTML(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("id")
	attemptID := r.PathValue("attempt")
	belongs, err := s.exports.AttemptBelongsTo(r.Context(), recordFromContext(r).Id, attemptID, campaignID)
	if err != nil {
		s.writeDownloadFailure(w, "load attempt", err)
		return
	}
	if !belongs {
		http.NotFound(w, r)
		return
	}
	data, filename, err := s.exports.AttemptHTML(r.Context(), recordFromContext(r).Id, attemptID)
	if err != nil {
		s.writeDownloadFailure(w, "build attempt html", err)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	s.writeDownload(w, "text/html; charset=utf-8", filename, data)
}

// handleCampaignDelete removes a confirmed inactive campaign and its history.
// An active claim or in-flight request refuses the deletion.
func (s *server) handleCampaignDelete(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	if strings.TrimSpace(r.FormValue("confirm")) != "delete" {
		page, err := s.campaignPageData(r, target, "", nil)
		if err != nil {
			s.campaignLoadFailure(w, r, err)
			return
		}
		page.DeleteError = "confirm the deletion before submitting"
		s.renderWithStatus(w, r, http.StatusBadRequest, views.CampaignPage(page))
		return
	}
	if err := s.retention.DeleteCampaign(r.Context(), recordFromContext(r).Id, target); err != nil {
		if errors.Is(err, campaign.ErrNotFound) || errors.Is(err, intake.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, retention.ErrCampaignActive) {
			page, dataErr := s.campaignPageData(r, target, "", nil)
			if dataErr != nil {
				s.campaignLoadFailure(w, r, dataErr)
				return
			}
			page.DeleteError = err.Error()
			s.renderWithStatus(w, r, http.StatusConflict, views.CampaignPage(page))
			return
		}
		s.app.Logger().Error("delete campaign", "campaign", target, "error", err)
		http.Error(w, "Unable to delete the campaign", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/campaigns", http.StatusSeeOther)
}

func (s *server) writeDownloadFailure(w http.ResponseWriter, operation string, err error) {
	if errors.Is(err, exports.ErrNotReady) || errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, campaign.ErrNotFound) || errors.Is(err, intake.ErrNotFound) {
		http.NotFound(w, nil)
		return
	}
	s.app.Logger().Error(operation, "error", err)
	http.Error(w, "Unable to load the requested download", http.StatusInternalServerError)
}

func (s *server) handleCampaignRevision(w http.ResponseWriter, r *http.Request) {
	// One 10 MiB document plus ~1 MiB of multipart framing: the body is
	// bounded before any part is read into memory.
	r.Body = http.MaxBytesReader(w, r.Body, campaign.MaxDocumentBytes+(1<<20))
	page, err := s.campaignPageData(r, r.PathValue("id"), "", nil)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	revisionError := func(message string) {
		page.RevisionError = message
		s.renderWithStatus(w, r, http.StatusBadRequest, views.CampaignPage(page))
	}

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			revisionError(convert.ErrInputTooLarge.Error())
		} else {
			revisionError("the form could not be read; upload a DOCX body document and try again")
		}
		return
	}
	subject := r.FormValue("subject")
	// The operator's own mapping text always wins over the derived draft when
	// the form is re-rendered with an error.
	if raw := r.FormValue("mappings"); strings.TrimSpace(raw) != "" {
		page.MappingDraft = raw
	}
	mappings, err := parseMappings(r.FormValue("mappings"))
	if err != nil {
		revisionError(err.Error())
		return
	}
	// Validate before conversion so every inline message is a fixed string
	// that names the offending variable; the service revalidates and only a
	// valid configuration ever becomes current.
	if err := campaign.ValidateSubject(subject); err != nil {
		revisionError(err.Error())
		return
	}
	if err := campaign.ValidateMappings(mappings); err != nil {
		revisionError(err.Error())
		return
	}
	file, header, err := r.FormFile("document")
	if err != nil {
		revisionError(campaign.ErrNoBody.Error())
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			s.app.Logger().Warn("close uploaded file", "error", err)
		}
	}()
	document, err := io.ReadAll(file)
	if err != nil {
		revisionError("the uploaded document could not be read; try again")
		return
	}

	_, warnings, err := s.campaigns.CreateRevision(r.Context(), recordFromContext(r).Id, page.ID, campaign.RevisionInput{
		Subject:      subject,
		Document:     document,
		DocumentName: header.Filename,
		Mappings:     mappings,
	})
	if err != nil {
		s.app.Logger().Error("create revision", "campaign", page.ID, "error", err)
		revisionError(revisionErrorMessage(err))
		return
	}
	// The warnings travel through the redirect so the refreshed page shows
	// what the conversion dropped; the fragment opens Template & preview.
	target := "/campaigns/" + page.ID
	if len(warnings) > 0 {
		target += "?" + url.Values{"warning": warnings}.Encode()
	}
	http.Redirect(w, r, target+"#template", http.StatusSeeOther)
}

func (s *server) handleCampaignLabels(w http.ResponseWriter, r *http.Request) {
	page, err := s.campaignPageData(r, r.PathValue("id"), "", nil)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	labels := splitLabels(r.FormValue("result_labels"))
	// Mirrors the service check so only the fixed validation message reaches
	// the form; storage failures after this are answered generically.
	if err := campaign.ValidateResultLabels(labels); err != nil {
		page.LabelsError = err.Error()
		page.ResultLabels = r.FormValue("result_labels")
		s.renderWithStatus(w, r, http.StatusBadRequest, views.CampaignPage(page))
		return
	}
	if err := s.campaigns.UpdateResultLabels(r.Context(), recordFromContext(r).Id, page.ID, labels); err != nil {
		s.app.Logger().Error("update result labels", "campaign", page.ID, "error", err)
		http.Error(w, "Unable to save result labels", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/campaigns/"+page.ID+"#settings", http.StatusSeeOther)
}

// handleCampaignPreview renders one in-memory sample row through the current
// revision. It stores nothing and creates no sending attempt: the typed render
// failures (missing required value, subject control character) are shown in
// the partial instead of becoming a server error.
func (s *server) handleCampaignPreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, id)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	data := views.PreviewData{}
	if loaded.CurrentRevisionID == "" {
		data.Error = "this campaign has no content revision yet; upload a body document first"
		s.renderWithStatus(w, r, http.StatusOK, views.PreviewResult(data))
		return
	}
	configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, loaded.CurrentRevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.campaignLoadFailure(w, r, err)
			return
		}
		s.app.Logger().Error("load revision for preview", "campaign", loaded.ID, "error", err)
		data.Error = "the stored revision could not be loaded for preview"
		s.renderWithStatus(w, r, http.StatusOK, views.PreviewResult(data))
		return
	}
	data.HasRevision = true
	data.Revision = configuration.Revision
	data.ContentHash = configuration.ContentHash
	data.Approved = configuration.ApprovalCurrent()

	sample := make(map[string]string, len(configuration.Mappings))
	for _, mapping := range configuration.Mappings {
		sample[mapping.Variable] = r.FormValue(mapping.Variable)
	}
	rendered, err := configuration.Compiled.Render(sample)
	if err != nil {
		data.Error = s.previewErrorMessage(err)
		s.renderWithStatus(w, r, http.StatusOK, views.PreviewResult(data))
		return
	}
	data.Recipient = rendered.Recipient
	data.Subject = rendered.Subject
	data.Body = rendered.Body
	for _, value := range rendered.Values {
		data.Values = append(data.Values, views.PreviewValue{
			Variable:   value.Variable,
			Header:     value.Header,
			Raw:        value.Raw,
			Normalized: value.Normalized,
		})
	}
	s.renderWithStatus(w, r, http.StatusOK, views.PreviewResult(data))
}

// campaignPageData builds the safe view model for one campaign workspace:
// header, the current revision, the four sections and their forms, and the
// immutable history. Warnings are the fixed inspection messages of the last
// conversion; section is the panel the server should render when no error
// applies.
func (s *server) campaignPageData(r *http.Request, id, section string, warnings []string) (views.CampaignPageData, error) {
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	data := views.CampaignPageData{ID: id, Section: section, Warnings: warnings}
	overview, err := s.loadCampaignOverview(ctx, ownerID, id)
	if err != nil {
		return data, err
	}
	data.Name = overview.Campaign.Name
	data.SourceHeaders = strings.Join(overview.Campaign.SourceHeaders, ", ")
	data.ResultLabels = strings.Join(overview.Campaign.ResultLabels, ", ")
	data.HasRevision = overview.HasRevision
	data.Revision = overview.Revision
	data.Approved = overview.Approved
	data.TasksError = overview.SummaryError
	// The help's worked example follows this campaign's own headers, so it never
	// teaches a variable name the draft spells differently.
	data.ExampleVariable, data.ExampleHeader = exampleMapping(overview.Campaign.SourceHeaders)

	// One revision query supplies both the revision history and each task's
	// saved revision number.
	history, err := s.campaigns.Revisions(ctx, ownerID, id)
	if err != nil {
		return data, err
	}
	revisionByConfiguration := make(map[string]int, len(history))
	for _, revision := range history {
		revisionByConfiguration[revision.ID] = revision.Revision
		approved := false
		if overview.HasRevision && revision.Revision == overview.Revision {
			approved = overview.Approved
		} else if approvedForRevision, err := s.sending.ApprovedFor(ctx, ownerID, revision.ID, revision.ContentHash); err == nil {
			approved = approvedForRevision
		}
		data.History = append(data.History, views.RevisionRow{
			Revision:    revision.Revision,
			ContentHash: revision.ContentHash,
			Approved:    approved,
		})
	}
	if configuration := overview.Configuration; configuration != nil {
		data.Subject = configuration.Subject
		data.Converter = configuration.Converter
		data.ContentHash = configuration.ContentHash
		for _, mapping := range configuration.Mappings {
			data.Variables = append(data.Variables, views.PreviewVariable{Name: mapping.Variable, Required: mapping.Required})
		}
		if len(configuration.Mappings) > 0 {
			data.ExampleVariable = configuration.Mappings[0].Variable
			data.ExampleHeader = configuration.Mappings[0].Header
		}
	} else {
		// A campaign without a revision has no operator mappings yet: offer the
		// source headers as the draft table.
		data.MappingDraft = mappingDraftText(campaign.DefaultMappings(overview.Campaign.SourceHeaders))
	}
	ids := make([]string, 0, len(overview.Tasks))
	for _, task := range overview.Tasks {
		ids = append(ids, task.Task.ID)
	}
	refs := presentation.ShortRefs(ids)
	for _, task := range overview.Tasks {
		row := campaignTaskRow(overview, task, revisionByConfiguration)
		row.Reference = refs[task.Task.ID]
		data.Tasks = append(data.Tasks, row)
	}

	artifacts, err := s.exports.CampaignArtifacts(ctx, ownerID, data.ID)
	if err != nil {
		return data, err
	}
	for _, artifact := range artifacts {
		switch artifact.Kind {
		case exports.KindCampaignReport:
			data.ReportState = artifact.State
			data.ReportError = artifact.Error
			data.ReportReady = artifact.Ready
			data.ReportAsOf = presentation.Timestamp(artifact.AsOf)
		case exports.KindCampaignPackage:
			data.PackageState = artifact.State
			data.PackageError = artifact.Error
			data.PackageReady = artifact.Ready
			data.PackageAsOf = presentation.Timestamp(artifact.AsOf)
		}
	}
	attempts, err := s.app.FindRecordsByFilter(sending.AttemptsCollection, "task.campaign={:c}", "-created,-id", 100, 0, dbx.Params{"c": data.ID})
	if err != nil {
		return data, err
	}
	for _, attempt := range attempts {
		data.Attempts = append(data.Attempts, views.AttemptRow{
			ID:         attempt.Id,
			Kind:       attempt.GetString("kind"),
			Recipient:  attempt.GetString("recipient"),
			Subject:    attempt.GetString("subject"),
			State:      attempt.GetString("state"),
			Reason:     attempt.GetString("reason"),
			FinishedAt: presentation.Timestamp(attempt.GetDateTime("finished_at").Time().UTC()),
			Test:       attempt.GetString("kind") == sending.KindTest,
		})
	}
	return data, nil
}

// campaignTaskRow maps one task's derived facts to the campaign Tasks section.
// The next-action link comes from the same presentation rule the task header
// uses, so the list and the task agree.
func campaignTaskRow(overview *campaignOverview, task taskOverview, revisions map[string]int) views.CampaignTaskRow {
	status := presentation.MainStatus(task.Facts)
	next := presentation.NextAction(task.Facts)
	filename := task.Task.SourceName
	if filename == "" {
		filename = "Workbook"
	}
	row := views.CampaignTaskRow{
		ID:               task.Task.ID,
		Reference:        presentation.ShortRef(task.Task.ID),
		Filename:         filename,
		UploadedAt:       presentation.Timestamp(task.Task.UploadedAt),
		Status:           status.Label,
		StatusClass:      statusClass(status),
		StatusDetail:     status.Explanation,
		Recipients:       task.Recipients.Label(),
		RecipientsDetail: task.Recipients.Details(),
		NextLabel:        sendingTypeNextLabel(next.Label, task.Task.SendingType),
		NextSection:      next.Section,
		NextDestination:  next.Destination,
		NeedsAttention:   presentation.NeedsAttention(task.Facts),
	}
	if revision, ok := revisions[task.Task.ConfigurationID]; ok && task.Task.ConfigurationID != "" {
		row.HasSavedRevision = true
		row.SavedRevision = revision
	}
	if overview.SummaryError != "" {
		row.Recipients = "Unavailable; retry"
		row.RecipientsDetail = nil
		row.StatusDetail = "Task metadata is unavailable; retry this page."
	}
	return row
}

// statusClass maps a derived status to the existing badge styling classes.
func statusClass(status presentation.TaskStatus) string {
	switch {
	case status.Warning:
		return "status-warning"
	case status.Success:
		return "status-success"
	default:
		return "status-muted"
	}
}

// campaignLoadFailure answers a failed campaign load without leaking storage
// detail: an unknown id is a plain 404, anything else is logged and answered
// generically.
func (s *server) campaignLoadFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, campaign.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.app.Logger().Error("load campaign", "error", err)
	http.Error(w, "Unable to load campaign", http.StatusInternalServerError)
}

// renderWithStatus buffers the component before writing so a validation
// failure can re-render the same page with an inline error and an explicit
// status. It exists beside web.go's render, which only answers 200.
func (s *server) renderWithStatus(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	var buf bytes.Buffer
	if err := component.Render(r.Context(), &buf); err != nil {
		s.app.Logger().Error("render campaign page", "error", err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
		return
	}
	// Every rendered page carries operator-private records: never let a
	// browser serve a stale copy after a state change.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		s.app.Logger().Warn("write page response", "error", err)
	}
}

// parseMappings reads the revision form's mapping textarea. The contract is:
// one mapping per line, fields split on "|", every field trimmed:
//
//	variable | source header | required|optional|recipient | format | fallback
//
// Column 3 defaults to "required". Column 4 must be one of text, name,
// designation or email and defaults to "text", or "email" for a recipient.
// Exactly one row must select the recipient. The fallback column is used
// only for optional rows. Blank lines are ignored. Malformed lines name their
// line number; campaign.ValidateMappings and campaign.Compile report the
// remaining problems inline on the form.
func parseMappings(raw string) ([]campaign.Mapping, error) {
	var mappings []campaign.Mapping
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		for j := range fields {
			fields[j] = strings.TrimSpace(fields[j])
		}
		if len(fields) < 2 || len(fields) > 5 {
			return nil, fmt.Errorf("mapping line %d: expected \"variable | source header | required|optional|recipient | format | fallback\"", i+1)
		}
		required := true
		recipient := false
		if len(fields) > 2 {
			switch fields[2] {
			case "", "required":
			case "optional":
				required = false
			case "recipient":
				recipient = true
			default:
				return nil, fmt.Errorf("mapping line %d: column 3 must be \"required\", \"optional\" or \"recipient\"", i+1)
			}
		}
		format := campaign.FormatText
		if recipient {
			format = campaign.FormatEmail
		}
		if len(fields) > 3 && fields[3] != "" {
			format = campaign.Format(fields[3])
		}
		var fallback string
		if len(fields) > 4 {
			fallback = fields[4]
		}
		mappings = append(mappings, campaign.Mapping{
			Variable:  fields[0],
			Header:    fields[1],
			Required:  required,
			Recipient: recipient,
			Format:    format,
			Fallback:  fallback,
		})
	}
	return mappings, nil
}

// mappingDraftText renders a derived draft in the textarea syntax
// parseMappings reads, one row per line, so the operator can edit the draft and
// mark its recipient row before submitting.
func mappingDraftText(mappings []campaign.Mapping) string {
	lines := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		lines = append(lines, strings.Join([]string{
			mapping.Variable,
			mapping.Header,
			"required",
			string(mapping.Format),
		}, " | "))
	}
	return strings.Join(lines, "\n")
}

// exampleMapping is the worked example shown beside the mapping form: the first
// row the campaign's own expected headers derive. A campaign whose headers
// cannot form a variable name falls back to a generic placeholder, so the
// example is never empty.
func exampleMapping(sourceHeaders []string) (variable, header string) {
	if drafted := campaign.DefaultMappings(sourceHeaders); len(drafted) > 0 {
		return drafted[0].Variable, drafted[0].Header
	}
	return "variable_name", "Header"
}

// campaignFormError returns the campaign package's fixed validation message
// for create input, or "" when valid. It mirrors CreateCampaign so storage
// failures never surface as inline form errors.
func campaignFormError(input campaign.CreateCampaignInput) string {
	switch {
	case strings.TrimSpace(input.Name) == "":
		return "a campaign name is required"
	case len(splitLabels(strings.Join(input.SourceHeaders, ","))) == 0:
		return "at least one source header is required for the mapping draft"
	}
	if err := campaign.ValidateResultLabels(campaign.NormalizeResultLabels(input.ResultLabels)); err != nil {
		return err.Error()
	}
	return ""
}

// splitLabels reads a comma-separated label field, trimmed and without empty
// entries, mirroring NormalizeResultLabels so the form and the service agree.
func splitLabels(raw string) []string {
	var labels []string
	for label := range strings.SplitSeq(raw, ",") {
		if label = strings.TrimSpace(label); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

// converterMessage turns a converter availability failure into a fixed
// operator-facing instruction. The raw error may quote the configured
// executable path, so it is never shown.
func converterMessage(err error) string {
	if errors.Is(err, convert.ErrConverterMissing) {
		return "The DOCX converter (pandoc) is not available. Install it or set PANDOC_PATH."
	}
	return "The DOCX converter is not working correctly."
}

// revisionErrorMessage decides which conversion failures may be shown inline.
// Typed campaign errors name the offending variable or subject with fixed
// text, and the converter and upload sentinels are fixed strings. Everything
// else (converter diagnostics, filesystem failures) was logged at the call
// site and is replaced so no path or diagnostic reaches the page.
func revisionErrorMessage(err error) string {
	var unknown *campaign.UnknownVariableError
	var unused *campaign.UnusedVariableError
	var missing *campaign.MissingRequiredError
	var control *campaign.SubjectControlError
	if errors.As(err, &unknown) || errors.As(err, &unused) || errors.As(err, &missing) || errors.As(err, &control) {
		return err.Error()
	}
	switch {
	case errors.Is(err, campaign.ErrNoBody):
		return campaign.ErrNoBody.Error()
	case errors.Is(err, campaign.ErrSubjectEmpty):
		return campaign.ErrSubjectEmpty.Error()
	case errors.Is(err, campaign.ErrNoMappings):
		return campaign.ErrNoMappings.Error()
	case errors.Is(err, convert.ErrConverterMissing):
		return converterMessage(err)
	case errors.Is(err, convert.ErrInputTooLarge):
		return convert.ErrInputTooLarge.Error()
	case errors.Is(err, convert.ErrOutputTooLarge):
		return convert.ErrOutputTooLarge.Error()
	case errors.Is(err, convert.ErrNotDocx):
		return convert.ErrNotDocx.Error()
	case errors.Is(err, convert.ErrNotAllowed):
		return convert.ErrNotAllowed.Error()
	}
	return "the document could not be converted into a revision; check the subject, the mapping table and the document, then try again"
}

// previewErrorMessage keeps typed campaign render failures inline: they name
// the missing variable or the subject problem with fixed text. Anything
// unexpected is logged and replaced so no internal detail reaches the partial.
func (s *server) previewErrorMessage(err error) string {
	var missing *campaign.MissingRequiredError
	var control *campaign.SubjectControlError
	var unknown *campaign.UnknownVariableError
	var unused *campaign.UnusedVariableError
	if errors.Is(err, campaign.ErrRecipientInvalid) {
		return err.Error()
	}
	if errors.As(err, &missing) || errors.As(err, &control) || errors.As(err, &unknown) || errors.As(err, &unused) {
		return err.Error()
	}
	s.app.Logger().Error("render campaign preview", "error", err)
	return "the preview could not be rendered with these sample values"
}
