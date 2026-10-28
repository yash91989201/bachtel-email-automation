package preparation

import (
	"context"
	"errors"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

// Inference is the call boundary of one normalization request. The durable
// worker binds the owner's confirmed credential inside CompleteFor; the
// temporary campaign sample binds the credential captured with its
// configuration. Neither boundary retries or falls back to another model.
type Inference func(ctx context.Context, config ai.Config, request ai.InferenceRequest) (string, error)

// operation is the normalization request shared by the durable worker and the
// temporary campaign sample: payload, one inference call and response
// validation. It owns no claim, persistence, retry or progress.
type operation struct {
	inference    Inference
	config       ai.Config
	prompt       string
	instructions string
	options      RunOptions
	session      string
}

// normalize sends one batch over the shared contract and returns its validated
// row results. The input describes every mapped field of the request; rows are
// the batch sent now.
func (o operation) normalize(ctx context.Context, input Input, rows []InputRow) ([]RowResult, error) {
	payload, err := MarshalPayload(input, o.instructions, rows)
	if err != nil {
		return nil, errors.Join(ErrInvalidResponse, err)
	}
	text, err := o.inference(ctx, o.config, ai.InferenceRequest{
		System:      o.prompt,
		User:        payload,
		MaxTokens:   o.options.MaxTokens,
		Temperature: o.options.Temperature,
		Session:     o.session,
	})
	if err != nil {
		return nil, err
	}
	batch := input
	batch.Rows = rows
	return ParseResponse(text, batch)
}
