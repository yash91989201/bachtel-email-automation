package web

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

type exportWebFixture struct {
	app        core.App
	handler    http.Handler
	cookie     *http.Cookie
	campaignID string
	taskID     string
}

func newExportWebFixture(t *testing.T) *exportWebFixture {
	t.Helper()
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Export web")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d body=%.200s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	return &exportWebFixture{app: app, handler: handler, cookie: cookie, campaignID: id, taskID: taskID}
}

func (f *exportWebFixture) setTaskStatus(t *testing.T, status string) {
	t.Helper()
	if _, err := f.app.DB().NewQuery("UPDATE send_tasks SET status = {:status} WHERE id = {:id}").
		Bind(dbx.Params{"status": status, "id": f.taskID}).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestExportDownloadsRequireOperatorAndReadiness(t *testing.T) {
	f := newExportWebFixture(t)
	results := "/campaigns/" + f.campaignID + "/tasks/" + f.taskID + "/results.xlsx"

	rec := do(t, f.handler, http.MethodGet, results, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous results download: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = do(t, f.handler, http.MethodGet, results, nil, withCookie(f.cookie))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("results download before generation: status=%d", rec.Code)
	}
	// A task that has not reached a terminal state cannot generate.
	rec = do(t, f.handler, http.MethodPost, strings.TrimSuffix(results, ".xlsx"), url.Values{}, withCookie(f.cookie))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "completed, stopped or recovered") {
		t.Fatalf("non-terminal generation: status=%d body=%.200s", rec.Code, rec.Body.String())
	}

	f.setTaskStatus(t, "completed")
	rec = do(t, f.handler, http.MethodPost, strings.TrimSuffix(results, ".xlsx"), url.Values{}, withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("generation: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	rec = do(t, f.handler, http.MethodGet, results, nil, withCookie(f.cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("results download: status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("downloaded workbook unreadable: %v", err)
	}
	if err := book.Close(); err != nil {
		t.Error(err)
	}

	// Direct campaign download URLs are behind the same operator session.
	report := "/campaigns/" + f.campaignID + "/exports/report.xlsx"
	rec = do(t, f.handler, http.MethodGet, report, nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous report download: status=%d", rec.Code)
	}
	rec = do(t, f.handler, http.MethodGet, report, nil, withCookie(f.cookie))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("report before generation: status=%d", rec.Code)
	}
	rec = do(t, f.handler, http.MethodPost, "/campaigns/"+f.campaignID+"/exports", url.Values{}, withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("campaign export generation: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	rec = do(t, f.handler, http.MethodGet, report, nil, withCookie(f.cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("report download: status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = do(t, f.handler, http.MethodGet, "/campaigns/"+f.campaignID+"/exports/package.zip", nil, withCookie(f.cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "zip") {
		t.Fatalf("package download: status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("PK")) {
		t.Fatal("package download is not a ZIP archive")
	}
}

func TestAttemptHTMLRequiresOwnershipAndOperator(t *testing.T) {
	f := newExportWebFixture(t)
	store := sending.NewStore(f.app, sending.StoreOptions{AllowCandidateSending: true, SenderAddress: "hr@example.invalid"})
	attempt, err := store.QueueTest(context.Background(), sending.TestRequest{
		ActionID:  "export-html-test-123456",
		TaskID:    f.taskID,
		Actor:     "operator",
		Recipient: "sample@example.invalid",
		Values:    map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.DB().NewQuery("UPDATE submission_attempts SET state='submitted', sender='hr@example.invalid', body='<p>Hello Sample</p>' WHERE id={:id}").
		Bind(dbx.Params{"id": attempt.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	target := "/campaigns/" + f.campaignID + "/attempts/" + attempt.ID + "/email.html"

	rec := do(t, f.handler, http.MethodGet, target, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous attempt HTML: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = do(t, f.handler, http.MethodGet, target, nil, withCookie(f.cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("attempt HTML: status=%d body=%.200s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("attempt HTML is missing its content policy")
	}
	body := rec.Body.String()
	for _, want := range []string{"Hello Sample", "hr@example.invalid", "sample@example.invalid"} {
		if !strings.Contains(body, want) {
			t.Fatalf("attempt HTML missing %q", want)
		}
	}
	// An attempt from another campaign is not reachable through this path.
	other := createCampaign(t, f.handler, f.cookie, "Other campaign")
	rec = do(t, f.handler, http.MethodGet, "/campaigns/"+other+"/attempts/"+attempt.ID+"/email.html", nil, withCookie(f.cookie))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign attempt HTML: status=%d", rec.Code)
	}
}

func TestCampaignDeleteRequiresConfirmationAndIsScoped(t *testing.T) {
	f := newExportWebFixture(t)
	other := createCampaign(t, f.handler, f.cookie, "Keep this campaign")
	// A generation exists so the delete has to remove stored files too.
	f.setTaskStatus(t, "completed")
	if rec := do(t, f.handler, http.MethodPost, "/campaigns/"+f.campaignID+"/tasks/"+f.taskID+"/results", url.Values{}, withCookie(f.cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare results: status=%d", rec.Code)
	}
	if rec := do(t, f.handler, http.MethodPost, "/campaigns/"+f.campaignID+"/exports", url.Values{}, withCookie(f.cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare exports: status=%d", rec.Code)
	}

	page := do(t, f.handler, http.MethodGet, "/campaigns/"+f.campaignID, nil, withCookie(f.cookie))
	for _, want := range []string{"duplicate-prevention history", "no automatic expiry", "backups that already exist"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("deletion page does not state %q before confirmation", want)
		}
	}

	target := "/campaigns/" + f.campaignID + "/delete"
	rec := do(t, f.handler, http.MethodPost, target, url.Values{}, withCookie(f.cookie))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "confirm the deletion") {
		t.Fatalf("unconfirmed delete: status=%d", rec.Code)
	}
	if _, err := f.app.FindRecordById(campaign.CampaignsCollection, f.campaignID); err != nil {
		t.Fatalf("campaign deleted without confirmation: %v", err)
	}

	rec = do(t, f.handler, http.MethodPost, target, url.Values{"confirm": {"delete"}}, withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/campaigns" {
		t.Fatalf("confirmed delete: status=%d location=%q body=%.200s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if _, err := f.app.FindRecordById(campaign.CampaignsCollection, f.campaignID); err == nil {
		t.Fatal("campaign record survived deletion")
	}
	for _, scope := range []struct {
		collection string
		filter     string
	}{
		{intake.TasksCollection, "campaign={:c}"},
		{intake.CandidatesCollection, "campaign={:c}"},
		{sending.AttemptsCollection, "task={:t}"},
		{exports.ArtifactsCollection, "campaign={:c}"},
		{campaign.ConfigurationsCollection, "campaign={:c}"},
	} {
		count, err := f.app.CountRecords(scope.collection, dbx.NewExp(scope.filter, dbx.Params{"c": f.campaignID, "t": f.taskID}))
		if err != nil {
			t.Fatalf("%s count: %v", scope.collection, err)
		}
		if count != 0 {
			t.Fatalf("%s kept %d records for the deleted campaign", scope.collection, count)
		}
	}
	if _, err := f.app.FindRecordById(campaign.CampaignsCollection, other); err != nil {
		t.Fatalf("unrelated campaign deleted: %v", err)
	}
}
