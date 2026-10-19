package campaign

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/convert"
)

// Service persists campaigns and immutable content revisions and owns the one
// conversion and rendering path. Nothing here sends email.
type Service struct {
	app       core.App
	converter convert.Converter
	policy    *bluemonday.Policy
}

// NewService returns the campaign service. An empty converterPath resolves the
// default converter, and a missing converter is reported when a body document
// is first needed rather than at startup.
func NewService(app core.App, converterPath string) *Service {
	return NewServiceWithConverter(app, convert.Converter{Path: converterPath})
}

// NewServiceWithConverter allows tests to inject an explicit converter.
func NewServiceWithConverter(app core.App, converter convert.Converter) *Service {
	return &Service{app: app, converter: converter, policy: NewSanitizer()}
}

// NewSanitizer returns the HTML allowlist applied to converted body documents.
// It is explicit: strict by default, with only the supported semantic elements
// and safe link targets re-enabled. Anything else is stripped, and an
// unsupported link scheme becomes inert text.
func NewSanitizer() *bluemonday.Policy {
	policy := bluemonday.StrictPolicy()
	policy.AllowElements(
		"p", "br",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li",
		"strong", "b", "em", "i", "u", "s", "sub", "sup",
		"blockquote", "code", "pre", "hr",
	)
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowStandardURLs()
	policy.RequireNoFollowOnLinks(false)
	policy.RequireParseableURLs(true)
	return policy
}

// CreateCampaignInput is the operator input for a new campaign.
type CreateCampaignInput struct {
	Name          string
	SourceHeaders []string
	ResultLabels  []string
}

// CreateCampaign stores a campaign with its output settings. The first content
// revision is created separately, when the operator uploads a body document.
func (s *Service) CreateCampaign(ctx context.Context, input CreateCampaignInput) (*Campaign, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("a campaign name is required")
	}
	sourceHeaders := make([]string, 0, len(input.SourceHeaders))
	for _, header := range input.SourceHeaders {
		if header = strings.TrimSpace(header); header != "" {
			sourceHeaders = append(sourceHeaders, header)
		}
	}
	if len(sourceHeaders) == 0 {
		return nil, errors.New("at least one source header is required for the mapping draft")
	}
	labels := NormalizeResultLabels(input.ResultLabels)
	if err := ValidateResultLabels(labels); err != nil {
		return nil, err
	}

	collection, err := s.app.FindCollectionByNameOrId(CampaignsCollection)
	if err != nil {
		return nil, fmt.Errorf("campaigns collection is missing; run migrations first: %w", err)
	}
	record := core.NewRecord(collection)
	record.Set("name", name)
	record.Set("source_headers", sourceHeaders)
	record.Set("result_labels", labels)
	if err := s.app.Save(record); err != nil {
		return nil, err
	}
	return s.campaignFromRecord(record), nil
}

// RevisionInput is the operator input for one content revision.
type RevisionInput struct {
	Subject string
	// Document is the uploaded DOCX body.
	Document []byte
	// DocumentName is the uploaded file name, used only for the stored file.
	DocumentName string
	Mappings     []Mapping
}

// CreateRevision converts the document, validates the mapping table against the
// placeholders actually used, and stores an immutable revision. A validation
// failure leaves the previous revision current: an invalid configuration never
// becomes usable. Returned warnings name unsupported document content.
func (s *Service) CreateRevision(ctx context.Context, campaignID string, input RevisionInput) (*Configuration, []string, error) {
	campaign, err := s.LoadCampaign(ctx, campaignID)
	if err != nil {
		return nil, nil, err
	}
	if len(input.Document) == 0 {
		return nil, nil, ErrNoBody
	}
	if len(input.Document) > MaxDocumentBytes {
		return nil, nil, convert.ErrInputTooLarge
	}
	subject := strings.TrimSpace(input.Subject)
	if err := ValidateSubject(subject); err != nil {
		return nil, nil, err
	}
	if err := ValidateMappings(input.Mappings); err != nil {
		return nil, nil, err
	}

	inspection := convert.InspectDocument(input.Document)
	if inspection.Err != nil {
		return nil, nil, inspection.Err
	}

	converterVersion, err := s.converter.Version(ctx)
	if err != nil {
		return nil, nil, err
	}
	converted, err := s.converter.DOCXToHTML(ctx, input.Document)
	if err != nil {
		return nil, nil, err
	}
	bodyHTML := s.policy.Sanitize(converted)

	template, err := Compile(subject, bodyHTML, input.Mappings)
	if err != nil {
		return nil, nil, err
	}

	document, err := filesystem.NewFileFromBytes(input.Document, documentName(input.DocumentName))
	if err != nil {
		return nil, nil, fmt.Errorf("store the uploaded document: %w", err)
	}

	collection, err := s.app.FindCollectionByNameOrId(ConfigurationsCollection)
	if err != nil {
		return nil, nil, fmt.Errorf("configurations collection is missing; run migrations first: %w", err)
	}
	revision, err := s.nextRevision(campaign.ID)
	if err != nil {
		return nil, nil, err
	}
	contentHash := ContentHash(subject, input.Document, input.Mappings, converterVersion)

	record := core.NewRecord(collection)
	record.Set("campaign", campaign.ID)
	record.Set("revision", revision)
	record.Set("subject", subject)
	record.Set("docx", document)
	record.Set("body_html", bodyHTML)
	record.Set("variables", template.Variables)
	record.Set("mappings", input.Mappings)
	record.Set("content_hash", contentHash)
	record.Set("converter", converterVersion)

	if err := s.app.RunInTransaction(func(txApp core.App) error {
		if err := txApp.Save(record); err != nil {
			return err
		}
		campaignRecord, err := txApp.FindRecordById(CampaignsCollection, campaign.ID)
		if err != nil {
			return err
		}
		campaignRecord.Set("current_configuration", record.Id)
		return txApp.Save(campaignRecord)
	}); err != nil {
		return nil, nil, err
	}

	configuration := s.configurationFromRecord(record)
	configuration.Compiled = template
	return configuration, inspection.Warnings, nil
}

// ListCampaigns returns campaigns ordered by name.
func (s *Service) ListCampaigns(ctx context.Context) ([]*Campaign, error) {
	records, err := s.app.FindRecordsByFilter(CampaignsCollection, "", "name", 0, 0)
	if err != nil {
		return nil, err
	}
	campaigns := make([]*Campaign, 0, len(records))
	for _, record := range records {
		campaigns = append(campaigns, s.campaignFromRecord(record))
	}
	return campaigns, nil
}

// LoadCampaign returns one campaign.
func (s *Service) LoadCampaign(ctx context.Context, id string) (*Campaign, error) {
	record, err := s.app.FindRecordById(CampaignsCollection, id)
	if err != nil {
		return nil, fmt.Errorf("campaign not found: %w", err)
	}
	return s.campaignFromRecord(record), nil
}

// CurrentConfiguration returns the revision a new task would use, compiled and
// ready to render. It reports an actionable error when the campaign has no
// revision yet.
func (s *Service) CurrentConfiguration(ctx context.Context, campaignID string) (*Configuration, error) {
	campaign, err := s.LoadCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if campaign.CurrentRevisionID == "" {
		return nil, fmt.Errorf("campaign %q has no configuration yet", campaign.Name)
	}
	return s.LoadConfiguration(ctx, campaign.CurrentRevisionID)
}

// LoadConfiguration loads and compiles one revision.
func (s *Service) LoadConfiguration(ctx context.Context, id string) (*Configuration, error) {
	record, err := s.app.FindRecordById(ConfigurationsCollection, id)
	if err != nil {
		return nil, fmt.Errorf("configuration not found: %w", err)
	}
	configuration := s.configurationFromRecord(record)
	template, err := Compile(configuration.Subject, configuration.BodyHTML, configuration.Mappings)
	if err != nil {
		return nil, fmt.Errorf("revision %d is not renderable: %w", configuration.Revision, err)
	}
	configuration.Compiled = template
	return configuration, nil
}

// Revisions returns every stored revision of a campaign, newest first.
func (s *Service) Revisions(ctx context.Context, campaignID string) ([]*Configuration, error) {
	records, err := s.app.FindRecordsByFilter(
		ConfigurationsCollection,
		"campaign = {:campaign}",
		"-revision",
		0,
		0,
		dbx.Params{"campaign": campaignID},
	)
	if err != nil {
		return nil, err
	}
	revisions := make([]*Configuration, 0, len(records))
	for _, record := range records {
		revisions = append(revisions, s.configurationFromRecord(record))
	}
	return revisions, nil
}

// UpdateResultLabels changes only the downloadable column labels. The content
// hash of the current revision is untouched, so content approval survives a
// presentation-only change.
func (s *Service) UpdateResultLabels(ctx context.Context, campaignID string, labels []string) error {
	if err := ValidateResultLabels(labels); err != nil {
		return err
	}
	record, err := s.app.FindRecordById(CampaignsCollection, campaignID)
	if err != nil {
		return fmt.Errorf("campaign not found: %w", err)
	}
	record.Set("result_labels", NormalizeResultLabels(labels))
	return s.app.Save(record)
}

// ConverterStatus reports whether a body document can currently be converted.
// The message is safe to show to the operator: it never contains a path.
func (s *Service) ConverterStatus(ctx context.Context) error {
	_, err := s.converter.Version(ctx)
	return err
}

func (s *Service) nextRevision(campaignID string) (int, error) {
	records, err := s.app.FindRecordsByFilter(
		ConfigurationsCollection,
		"campaign = {:campaign}",
		"-revision",
		1,
		0,
		dbx.Params{"campaign": campaignID},
	)
	if err != nil {
		return 0, err
	}
	highest := 0
	if len(records) > 0 {
		highest = records[0].GetInt("revision")
	}
	return highest + 1, nil
}

func (s *Service) campaignFromRecord(record *core.Record) *Campaign {
	campaign := &Campaign{
		ID:                record.Id,
		Name:              record.GetString("name"),
		CurrentRevisionID: record.GetString("current_configuration"),
	}
	if err := record.UnmarshalJSONField("source_headers", &campaign.SourceHeaders); err != nil {
		s.app.Logger().Warn("decode campaign source headers", "campaign", record.Id, "error", err)
		campaign.SourceHeaders = nil
	}
	if err := record.UnmarshalJSONField("result_labels", &campaign.ResultLabels); err != nil || len(campaign.ResultLabels) == 0 {
		campaign.ResultLabels = DefaultResultLabels
	}
	if campaign.CurrentRevisionID != "" {
		if revision, err := s.app.FindRecordById(ConfigurationsCollection, campaign.CurrentRevisionID); err == nil {
			campaign.Revision = revision.GetInt("revision")
		}
	}
	return campaign
}

func (s *Service) configurationFromRecord(record *core.Record) *Configuration {
	configuration := &Configuration{
		ID:           record.Id,
		CampaignID:   record.GetString("campaign"),
		Revision:     record.GetInt("revision"),
		Subject:      record.GetString("subject"),
		BodyHTML:     record.GetString("body_html"),
		ContentHash:  record.GetString("content_hash"),
		Converter:    record.GetString("converter"),
		ApprovedHash: record.GetString("approved_hash"),
	}
	if err := record.UnmarshalJSONField("mappings", &configuration.Mappings); err != nil {
		configuration.Mappings = nil
	}
	if err := record.UnmarshalJSONField("variables", &configuration.Variables); err != nil {
		configuration.Variables = nil
	}
	return configuration
}

// documentName keeps the uploaded name but forces a DOCX extension, so the
// stored private file never advertises a different format.
func documentName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	switch {
	case base == "", base == ".", base == "/":
		return "body.docx"
	case !strings.HasSuffix(strings.ToLower(base), ".docx"):
		return base + ".docx"
	default:
		return base
	}
}
