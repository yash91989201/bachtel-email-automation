package main

import (
	"context"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// connectionResolver binds a loaded mailbox connection to its concrete sender.
// It is the only provider-selection point: an unknown provider fails closed
// before any queue or credential mutation, and credentials are read only for
// the connection that is about to transmit.
type connectionResolver struct {
	cfg         config.Config
	connections *mailbox.Store
	microsoft   *microsoft.Service
}

func (r *connectionResolver) Sender(ctx context.Context, connectionID string) (sending.Sender, error) {
	connection, err := r.connections.ByID(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	switch connection.Provider {
	case mailbox.ProviderMicrosoft:
		return sending.NewGraph(microsoftTokens{service: r.microsoft, connection: connection}, sending.GraphOptions{
			BaseURL: r.cfg.Microsoft.GraphBaseURL,
			Timeout: 20 * time.Second,
		}), nil
	case mailbox.ProviderTitan:
		credential, err := r.connections.Credential(ctx, connection)
		if err != nil {
			return nil, err
		}
		return sending.NewTitan(r.cfg.Titan.Address, connection.Sender, credential.Password, sending.TitanOptions{
			Timeout: 20 * time.Second,
		}), nil
	default:
		return nil, mailbox.ErrUnsupportedProvider
	}
}

// microsoftTokens adapts one connection's token refresh to the Graph sender's
// TokenSource boundary.
type microsoftTokens struct {
	service    *microsoft.Service
	connection *mailbox.Connection
}

func (t microsoftTokens) AccessToken(ctx context.Context) (string, error) {
	return t.service.AccessToken(ctx, t.connection)
}
