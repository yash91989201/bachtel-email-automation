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
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
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
	// No candidate-send surface exists: GET /campaigns/{id} would otherwise
	// make the mux answer 405 (path match, method mismatch) for this path, so
	// the not-found answer is pinned explicitly.
	mux.HandleFunc("POST /campaigns/send", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}

func (s *server) handleCampaignList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := views.CampaignListData{}
	campaigns, err := s.campaigns.ListCampaigns(ctx)
	if err != nil {
		s.app.Logger().Error("list campaigns", "error", err)
		http.Error(w, "Unable to load campaigns", http.StatusInternalServerError)
		return
	}
	for _, c := range campaigns {
		row := views.CampaignListRow{ID: c.ID, Name: c.Name, SourceHeaders: strings.Join(c.SourceHeaders, ", ")}
		if c.CurrentRevisionID != "" {
			row.HasRevision = true
			row.Revision = c.Revision
			configuration, err := s.campaigns.LoadConfiguration(ctx, c.CurrentRevisionID)
			if err != nil {
				s.app.Logger().Error("load current configuration", "campaign", c.ID, "error", err)
			} else {
				row.Approved = configuration.ApprovalCurrent()
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
	created, err := s.campaigns.CreateCampaign(r.Context(), input)
	if err != nil {
		s.app.Logger().Error("create campaign", "error", err)
		http.Error(w, "Unable to create campaign", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/campaigns/"+created.ID, http.StatusSeeOther)
}

func (s *server) handleCampaignDetail(w http.ResponseWriter, r *http.Request) {
	data, err := s.campaignPageData(r, r.PathValue("id"), r.URL.Query()["warning"])
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.CampaignPage(data))
}

func (s *server) handleCampaignRevision(w http.ResponseWriter, r *http.Request) {
	// One 10 MiB document plus ~1 MiB of multipart framing: the body is
	// bounded before any part is read into memory.
	r.Body = http.MaxBytesReader(w, r.Body, campaign.MaxDocumentBytes+(1<<20))
	page, err := s.campaignPageData(r, r.PathValue("id"), nil)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	revisionError := func(message string) {
		page.RevisionError = message
		s.renderWithStatus(w, r, http.StatusBadRequest, views.CampaignPage(page))
	}

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
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
	defer func() { _ = file.Close() }()
	document, err := io.ReadAll(file)
	if err != nil {
		revisionError("the uploaded document could not be read; try again")
		return
	}

	_, warnings, err := s.campaigns.CreateRevision(r.Context(), page.ID, campaign.RevisionInput{
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
	// what the conversion dropped.
	target := "/campaigns/" + page.ID
	if len(warnings) > 0 {
		target += "?" + url.Values{"warning": warnings}.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *server) handleCampaignLabels(w http.ResponseWriter, r *http.Request) {
	page, err := s.campaignPageData(r, r.PathValue("id"), nil)
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
	if err := s.campaigns.UpdateResultLabels(r.Context(), page.ID, labels); err != nil {
		s.app.Logger().Error("update result labels", "campaign", page.ID, "error", err)
		http.Error(w, "Unable to save result labels", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/campaigns/"+page.ID, http.StatusSeeOther)
}

// handleCampaignPreview renders one in-memory sample row through the current
// revision. It stores nothing and creates no sending attempt: the typed render
// failures (missing required value, subject control character) are shown in
// the partial instead of becoming a server error.
func (s *server) handleCampaignPreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	loaded, err := s.campaigns.LoadCampaign(ctx, id)
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
	configuration, err := s.campaigns.LoadConfiguration(ctx, loaded.CurrentRevisionID)
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

// campaignPageData builds the safe view model for one campaign page: header,
// current revision, the revision, labels and preview forms, and the immutable
// history. Warnings are the fixed inspection messages of the last conversion,
// passed by the caller.
func (s *server) campaignPageData(r *http.Request, id string, warnings []string) (views.CampaignPageData, error) {
	ctx := r.Context()
	data := views.CampaignPageData{Warnings: warnings}
	loaded, err := s.campaigns.LoadCampaign(ctx, id)
	if err != nil {
		return data, err
	}
	data.ID = loaded.ID
	data.Name = loaded.Name
	data.SourceHeaders = strings.Join(loaded.SourceHeaders, ", ")
	data.ResultLabels = strings.Join(loaded.ResultLabels, ", ")
	// The help's worked example follows this campaign's own headers, so it never
	// teaches a variable name the draft spells differently.
	data.ExampleVariable, data.ExampleHeader = exampleMapping(loaded.SourceHeaders)
	if loaded.CurrentRevisionID != "" {
		configuration, err := s.campaigns.LoadConfiguration(ctx, loaded.CurrentRevisionID)
		if err != nil {
			return data, err
		}
		data.HasRevision = true
		data.Revision = configuration.Revision
		data.Subject = configuration.Subject
		data.Converter = configuration.Converter
		data.ContentHash = configuration.ContentHash
		data.Approved = configuration.ApprovalCurrent()
		for _, mapping := range configuration.Mappings {
			data.Variables = append(data.Variables, mapping.Variable)
		}
	} else {
		// A campaign without a revision has no operator mappings yet: offer the
		// source headers as the draft table.
		data.MappingDraft = mappingDraftText(campaign.DefaultMappings(loaded.SourceHeaders))
	}
	history, err := s.campaigns.Revisions(ctx, data.ID)
	if err != nil {
		return data, err
	}
	for _, revision := range history {
		data.History = append(data.History, views.RevisionRow{
			Revision:    revision.Revision,
			ContentHash: revision.ContentHash,
			Approved:    revision.ApprovalCurrent(),
		})
	}
	return data, nil
}

// campaignLoadFailure answers a failed campaign load without leaking storage
// detail: an unknown id is a plain 404, anything else is logged and answered
// generically.
func (s *server) campaignLoadFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
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
	for _, label := range strings.Split(raw, ",") {
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
