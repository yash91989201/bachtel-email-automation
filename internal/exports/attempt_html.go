package exports

import (
	"bytes"
	"html/template"

	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// attemptPage is a static report of one recorded attempt. Only Body is
// inserted as HTML: it is the already sanitized rendered body stored at send
// time. Every metadata value is escaped by html/template.
var attemptPage = template.Must(template.New("attempt").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Subject}}</title>
<style>
body{font-family:system-ui,sans-serif;margin:2rem auto;max-width:48rem;padding:0 1rem;color:#111}
dl{display:grid;grid-template-columns:max-content 1fr;gap:.25rem 1rem}
dt{font-weight:600}
dd{margin:0;word-break:break-word}
.body{margin-top:1.5rem;border-top:1px solid #ddd;padding-top:1.5rem}
pre{white-space:pre-wrap}
</style>
</head>
<body>
<h1>Recorded email attempt</h1>
<dl>
<dt>Attempt</dt><dd>{{.ID}}</dd>
<dt>Kind</dt><dd>{{.KindLabel}}</dd>
<dt>Outcome</dt><dd>{{.StateLabel}}</dd>
<dt>Recipient</dt><dd>{{.Recipient}}</dd>
<dt>Sender</dt><dd>{{.Sender}}</dd>
<dt>Subject</dt><dd>{{.Subject}}</dd>
<dt>Task</dt><dd>{{.TaskID}}</dd>
<dt>Configuration</dt><dd>{{.ConfigurationID}}</dd>
<dt>Candidate</dt><dd>{{.CandidateID}}</dd>
<dt>Started</dt><dd>{{.Started}}</dd>
<dt>Finished</dt><dd>{{.Finished}}</dd>
<dt>Retry count</dt><dd>{{.RetryCount}}</dd>
<dt>Content hash</dt><dd>{{.ContentHash}}</dd>
<dt>Reason</dt><dd>{{.Reason}}</dd>
</dl>
<div class="body">{{.Body}}</div>
</body>
</html>
`))

type attemptView struct {
	Body                              template.HTML
	ID, KindLabel, StateLabel, Sender string
	Recipient, Subject, TaskID        string
	ConfigurationID, CandidateID      string
	Started, Finished, ContentHash    string
	Reason                            string
	RetryCount                        int
}

// attemptHTML renders one recorded attempt. Only Body is inserted as HTML: it
// is the already sanitized rendered body stored at send time. The sender comes
// from the attempt record (the mailbox at queue time), never from current
// configuration.
func attemptHTML(attempt *core.Record) ([]byte, error) {
	view := attemptView{
		Body:            template.HTML(attempt.GetString("body")),
		ID:              attempt.Id,
		KindLabel:       kindLabel(attempt.GetString("kind")),
		StateLabel:      stateLabel(attempt.GetString("state")),
		Sender:          attempt.GetString("sender"),
		Recipient:       attempt.GetString("recipient"),
		Subject:         attempt.GetString("subject"),
		TaskID:          attempt.GetString("task"),
		ConfigurationID: attempt.GetString("configuration"),
		CandidateID:     attempt.GetString("candidate"),
		Started:         sending.DisplayTime(attempt.GetDateTime("started_at").Time().UTC()),
		Finished:        sending.DisplayTime(attempt.GetDateTime("finished_at").Time().UTC()),
		RetryCount:      attempt.GetInt("retry_count"),
		ContentHash:     attempt.GetString("content_hash"),
		Reason:          attempt.GetString("reason"),
	}
	var out bytes.Buffer
	if err := attemptPage.Execute(&out, view); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func kindLabel(kind string) string {
	if kind == sending.KindTest {
		return "Test submission"
	}
	return "Candidate submission"
}

func stateLabel(state string) string {
	switch state {
	case sending.AttemptQueued:
		return "Queued"
	case sending.AttemptInFlight:
		return "In flight"
	case sending.AttemptSubmitted:
		return "Provider accepted"
	case sending.AttemptFailed:
		return "Definite failure"
	case sending.AttemptReview:
		return "Uncertain - review required"
	case sending.AttemptRetryable:
		return "Retryable failure"
	case sending.AttemptAuth:
		return "Authorization failure"
	default:
		return state
	}
}
