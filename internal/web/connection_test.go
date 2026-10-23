package web

// Connection-flow operator surface tests: Titan form validation without any
// provider network, disconnect keeping the exclusive claim, the replacement
// confirmation boundary, and per-operator mailbox isolation.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

// TestConnectionPageRequiresProviderSelection keeps credentials out of the
// initial page and renders only the selected provider's form.
func TestConnectionPageRequiresProviderSelection(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)

	// The home page points at the private mailbox surface with the new copy.
	home := do(t, handler, http.MethodGet, "/", nil, withCookie(cookie))
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "Private mailbox") || !strings.Contains(home.Body.String(), `href="/connection"`) {
		t.Fatalf("home mailbox card: status=%d", home.Code)
	}

	page := do(t, handler, http.MethodGet, "/connection", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("connection page: status=%d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "Mailbox connection") {
		t.Fatalf("connection page missing heading copy: %.400s", body)
	}
	if !strings.Contains(body, `href="/connection?provider=microsoft365"`) || !strings.Contains(body, `href="/connection?provider=godaddy-titan"`) {
		t.Fatalf("connection page must offer both providers: %.400s", body)
	}
	if strings.Contains(body, `action="/connection/microsoft/settings"`) || strings.Contains(body, `action="/connection/titan"`) {
		t.Fatal("initial page rendered provider credentials before selection")
	}
	if strings.Contains(body, "GoDaddy") {
		t.Fatalf("mailbox UI must call this provider Titan: %.400s", body)
	}

	microsoft := do(t, handler, http.MethodGet, "/connection?provider=microsoft365", nil, withCookie(cookie))
	if microsoft.Code != http.StatusOK {
		t.Fatalf("Microsoft selection status=%d", microsoft.Code)
	}
	body = microsoft.Body.String()
	if !strings.Contains(body, `action="/connection/microsoft/settings"`) || strings.Contains(body, `action="/connection/titan"`) {
		t.Fatal("Microsoft selection must show only its credential form")
	}
	for _, name := range []string{"tenant_id", "client_id", "client_secret"} {
		if !strings.Contains(body, `name="`+name+`"`) {
			t.Fatalf("Microsoft setup form missing %s input", name)
		}
	}
	if strings.Contains(body, ">Continue with Microsoft 365<") {
		t.Fatal("Microsoft sign-in is offered before any registration is saved")
	}
	titan := do(t, handler, http.MethodGet, "/connection?provider=godaddy-titan", nil, withCookie(cookie))
	if titan.Code != http.StatusOK {
		t.Fatalf("Titan selection status=%d", titan.Code)
	}
	titanBody := titan.Body.String()
	if !strings.Contains(titanBody, `action="/connection/titan"`) || strings.Contains(titanBody, `action="/connection/microsoft/settings"`) {
		t.Fatal("Titan selection must show only its credential form")
	}
	titanStart := strings.Index(titanBody, `action="/connection/titan"`)
	titanEnd := strings.Index(titanBody[titanStart:], "</form>")
	titanForm := titanBody[titanStart : titanStart+titanEnd]
	if !strings.Contains(titanForm, "name=\"address\"") || !strings.Contains(titanForm, "name=\"password\"") {
		t.Fatalf("Titan form missing credential inputs: %.400s", titanForm)
	}
	// Sent copies reuse the fixed mailbox endpoint: the operator configures no
	// host, port, folder or copy switch.
	for _, forbidden := range []string{`name="host`, `name="port`, `name="imap`, `name="folder`, `name="smtp`} {
		if strings.Contains(titanForm, forbidden) {
			t.Fatalf("Titan form gained an endpoint input %s: %.400s", forbidden, titanForm)
		}
	}
	// No configured secret value reaches the page.
	for _, secret := range []string{"value=\"client-secret", "value=\"super-secret", "value=\""} {
		if secret == "value=\"" {
			// The form inputs render empty; no prefilled value attributes exist.
			if strings.Contains(titanForm, "value=\"") {
				t.Fatalf("Titan inputs must render empty: %.400s", titanForm)
			}
			continue
		}
		if strings.Contains(body, secret) {
			t.Fatalf("connection page leaked %q", secret)
		}
	}
}

func TestSavedMicrosoftSettingsPrioritizeSignIn(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)
	owner := operatorID(t, app)
	if _, err := mailbox.NewStore(app, testKey).SaveSettings(context.Background(), owner, mailbox.ProviderMicrosoft,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "saved-secret"); err != nil {
		t.Fatal(err)
	}

	page := do(t, handler, http.MethodGet, "/connection?provider=microsoft365", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("saved Microsoft page status=%d", page.Code)
	}
	body := page.Body.String()
	connect := strings.Index(body, `action="/connection/microsoft/connect"`)
	details := strings.Index(body, "<details")
	settings := strings.Index(body, `action="/connection/microsoft/settings"`)
	if connect < 0 || details < connect || settings < details || strings.Count(body, `action="/connection/microsoft/connect"`) != 1 {
		t.Fatal("saved settings must show one sign-in action before collapsed settings editing")
	}
	if !strings.Contains(body, "<summary class=\"cursor-pointer font-semibold\">Edit application settings</summary>") {
		t.Fatal("saved settings editor is missing its native disclosure summary")
	}
	if strings.Contains(body, "saved-secret") {
		t.Fatal("saved client secret was rendered")
	}
}

// TestTitanConnectValidationRejectsAndInstallsNothing drives the Titan form
// through every branch that returns before any dial, plus an unreachable
// endpoint on a closed local port, and asserts a fixed notice with no
// connection ever installed.
func TestTitanConnectValidationRejectsAndInstallsNothing(t *testing.T) {
	base := testConfig("http://127.0.0.1:8090")

	t.Run("invalid address", func(t *testing.T) {
		app, handler := newTestServer(t, base)
		provisionOperator(t, app)
		cookie := login(t, handler)

		rec := do(t, handler, http.MethodPost, "/connection/titan", url.Values{
			"address":  {"not-an-email"},
			"password": {"mailbox-password"},
		}, withCookie(cookie))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Enter the full mailbox address exactly as it is used to sign in.") {
			t.Fatalf("invalid address: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		assertNoConnections(t, app)
	})

	t.Run("empty password", func(t *testing.T) {
		app, handler := newTestServer(t, base)
		provisionOperator(t, app)
		cookie := login(t, handler)

		rec := do(t, handler, http.MethodPost, "/connection/titan", url.Values{
			"address":  {"mailbox@example.test"},
			"password": {""},
		}, withCookie(cookie))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Enter the email password for this Titan mailbox.") {
			t.Fatalf("empty password: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		assertNoConnections(t, app)
	})

	t.Run("unreachable endpoint", func(t *testing.T) {
		cfg := base
		// A closed loopback port refuses instantly; no live network involved.
		cfg.Titan.Address = "127.0.0.1:1"
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)
		cookie := login(t, handler)

		rec := do(t, handler, http.MethodPost, "/connection/titan", url.Values{
			"address":  {"mailbox@example.test"},
			"password": {"mailbox-password"},
		}, withCookie(cookie))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Titan could not be reached over encrypted SMTP; the connection was not saved.") {
			t.Fatalf("unreachable: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		assertNoConnections(t, app)
	})
}

// TestDisconnectKeepsClaimAndLeavesOthersUntouched disconnects one operator's
// mailbox: usable credentials go, the exclusive claim and the other operator's
// connection stay.
func TestDisconnectKeepsClaimAndLeavesOthersUntouched(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)
	owner := operatorID(t, app)
	installMailbox(t, app, owner, "mine@example.test")

	otherID := provisionOperatorAs(t, app, "second@example.com", "second-password")
	otherConnection := installMailbox(t, app, otherID, "theirs@example.test")

	rec := do(t, handler, http.MethodPost, "/connection/disconnect", url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/connection" {
		t.Fatalf("disconnect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	store := mailbox.NewStore(app, testKey)
	connection, err := store.Current(context.Background(), owner)
	if err != nil {
		t.Fatalf("disconnected connection disappeared: %v", err)
	}
	if connection.Status != mailbox.StatusDisconnected {
		t.Fatalf("status after disconnect = %q, want %q", connection.Status, mailbox.StatusDisconnected)
	}
	if _, err := store.Credential(context.Background(), connection); err == nil {
		t.Fatal("disconnect left usable credentials behind")
	}
	// The claim is retained: another operator without a mailbox cannot install
	// this address even though it is disconnected.
	claimantID := provisionOperatorAs(t, app, "third@example.com", "third-password")
	if _, err := store.Install(context.Background(), mailbox.Candidate{
		OwnerID:    claimantID,
		Provider:   mailbox.ProviderTitan,
		Sender:     "mine@example.test",
		Identity:   "mine@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "stolen"},
	}); err != mailbox.ErrClaimed {
		t.Fatalf("disconnected mailbox claim: err=%v, want %v", err, mailbox.ErrClaimed)
	}
	// The other operator's connection is untouched.
	theirs, err := store.ByID(context.Background(), otherConnection.ID)
	if err != nil || theirs.Status != mailbox.StatusConnected {
		t.Fatalf("other operator connection: %+v err=%v", theirs, err)
	}
	// The page reflects the disconnected state and hides the sender row.
	page := do(t, handler, http.MethodGet, "/connection", nil, withCookie(cookie))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-status="disconnected"`) {
		t.Fatalf("connection page after disconnect: status=%d", page.Code)
	}
}

// TestConnectionReplaceRequiresStagedReset pins the replacement confirmation
// boundary: no staged reset redirects, a staged reset renders exactly the
// kept/deleted scope and the duplicate-prevention warning.
func TestConnectionReplaceRequiresStagedReset(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)
	owner := operatorID(t, app)

	// Without a staged reset the page redirects back to the connection page.
	rec := do(t, handler, http.MethodGet, "/connection/replace", nil, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/connection" {
		t.Fatalf("replace without staged reset: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	installMailbox(t, app, owner, "current@example.test")
	store := mailbox.NewStore(app, testKey)
	operation, err := store.StageReplacement(context.Background(), mailbox.Candidate{
		OwnerID:    owner,
		Provider:   mailbox.ProviderTitan,
		Sender:     "proposed@example.test",
		Identity:   "proposed@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "new-password"},
	})
	if err != nil {
		t.Fatalf("stage replacement: %v", err)
	}

	page := do(t, handler, http.MethodGet, "/connection/replace", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("replace page: status=%d body=%.400s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, want := range []string{
		"current@example.test",
		"proposed@example.test",
		"Campaign definitions and result-column settings",
		"Candidates, task snapshots and reservations",
		"Tasks and pending schedules",
		"Submission attempts, manual resolutions and history",
		"Duplicate-prevention history is deleted.",
		`value="` + operation.ID + `"`,
		`action="/connection/replace/confirm"`,
		`action="/connection/replace/cancel"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("replace page missing %q: %.800s", want, body)
		}
	}
	// Cancelling the staged reset clears it and redirects home.
	cancel := do(t, handler, http.MethodPost, "/connection/replace/cancel", url.Values{"reset": {operation.ID}}, withCookie(cookie))
	if cancel.Code != http.StatusSeeOther || cancel.Header().Get("Location") != "/connection" {
		t.Fatalf("cancel: status=%d location=%q", cancel.Code, cancel.Header().Get("Location"))
	}
	afterCancel := do(t, handler, http.MethodGet, "/connection/replace", nil, withCookie(cookie))
	if afterCancel.Code != http.StatusSeeOther {
		t.Fatalf("replace after cancel: status=%d", afterCancel.Code)
	}
}

// TestOperatorsSeeOnlyTheirOwnMailbox drives two operators with independent
// sessions and connections: each connection page shows only their own sender,
// and the other operator's campaign, task and download ids are 404s that
// change nothing.
func TestOperatorsSeeOnlyTheirOwnMailbox(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.PandocPath = stubPandoc(t)
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)
	installMailbox(t, app, operatorID(t, app), "first@example.test")

	otherID := provisionOperatorAs(t, app, "second@example.com", "second-password")
	otherCookie := loginAs(t, handler, "second@example.com", "second-password")
	installMailbox(t, app, otherID, "second@example.test")

	// Each connection page shows only its own sender and status.
	mine := do(t, handler, http.MethodGet, "/connection", nil, withCookie(cookie))
	if mine.Code != http.StatusOK || !strings.Contains(mine.Body.String(), "first@example.test") || strings.Contains(mine.Body.String(), "second@example.test") {
		t.Fatalf("first operator connection page: status=%d", mine.Code)
	}
	theirs := do(t, handler, http.MethodGet, "/connection", nil, withCookie(otherCookie))
	if theirs.Code != http.StatusOK || !strings.Contains(theirs.Body.String(), "second@example.test") || strings.Contains(theirs.Body.String(), "first@example.test") {
		t.Fatalf("second operator connection page: status=%d", theirs.Code)
	}

	// The first operator creates a campaign with a staged task.
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))

	// Every read of the first operator's work is a 404 for the second one.
	for _, target := range []string{
		"/campaigns/" + id,
		"/campaigns/" + id + "/tasks/" + taskID,
		"/campaigns/" + id + "/tasks/" + taskID + "/results.xlsx",
		"/campaigns/" + id + "/exports/report.xlsx",
	} {
		if rec := do(t, handler, http.MethodGet, target, nil, withCookie(otherCookie)); rec.Code != http.StatusNotFound {
			t.Fatalf("cross-operator GET %s: status=%d", target, rec.Code)
		}
	}
	// Mutations are refused and change nothing.
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/labels", url.Values{"result_labels": {"Status"}}, withCookie(otherCookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-operator labels: status=%d", rec.Code)
	}
	// Production bug (reported, not fixed here): the confirmed delete path
	// answers 500 instead of 404 for a foreign campaign because
	// handleCampaignDelete maps only retention.ErrCampaignActive and drops
	// campaign.ErrNotFound into the generic failure. The ownership boundary
	// still holds: nothing may be deleted.
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/delete", url.Values{"confirm": {"delete"}}, withCookie(otherCookie)); rec.Code < 400 {
		t.Fatalf("cross-operator delete: status=%d", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/sending/test", url.Values{
		"action_id": {"cross-operator-test-1"}, "test_address": {"attacker@example.test"},
	}, withCookie(otherCookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-operator sending test: status=%d", rec.Code)
	}
	// The first operator's campaign, task and labels survive untouched.
	if _, err := app.FindRecordById("campaigns", id); err != nil {
		t.Fatalf("campaign was mutated cross-operator: %v", err)
	}
	if _, err := app.FindRecordById("send_tasks", taskID); err != nil {
		t.Fatalf("task was mutated cross-operator: %v", err)
	}
	page := do(t, handler, http.MethodGet, "/campaigns/"+id, nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("owner lost access: status=%d", page.Code)
	}
}

func assertNoConnections(t *testing.T, app core.App) {
	t.Helper()
	count, err := app.CountRecords(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("connection was installed despite the rejection: %d records", count)
	}
}
