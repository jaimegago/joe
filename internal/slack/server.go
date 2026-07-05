package slack

import (
	"context"
	"log/slog"

	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// Server is the Joe Slack bot. It listens for Slack events via Socket Mode
// and routes them to the Handler.
type Server struct {
	api     *gslack.Client
	sm      *socketmode.Client
	handler *Handler
}

// NewServer creates a Server.
func NewServer(api *gslack.Client, sm *socketmode.Client, agent *Agent) *Server {
	return &Server{
		api:     api,
		sm:      sm,
		handler: NewHandler(api, agent, NewFormatter()),
	}
}

// Start begins listening for Slack events. It blocks until ctx is cancelled or
// the Socket Mode connection fails.
func (s *Server) Start(ctx context.Context) error {
	go s.processEvents(ctx)

	// RunContext (not Run) so a ctx cancel tears the Socket Mode websocket
	// down and lets this goroutine exit — with plain Run the connection and
	// goroutine would outlive Start and leak on shutdown.
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.sm.RunContext(ctx)
	}()

	select {
	case <-ctx.Done():
		// Our own cancellation: RunContext is unwinding; its error is not a
		// connection failure worth surfacing.
		return nil
	case err := <-errCh:
		return err
	}
}

// processEvents reads events from the Socket Mode client and dispatches them.
func (s *Server) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-s.sm.Events:
			if !ok {
				return
			}
			s.dispatch(ctx, evt)
		}
	}
}

func (s *Server) dispatch(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		slog.Info("joe-slack: connecting to Slack")

	case socketmode.EventTypeConnected:
		slog.Info("joe-slack: connected to Slack")

	case socketmode.EventTypeHello:
		// Nothing to do.

	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(gslack.SlashCommand)
		if !ok {
			return
		}
		s.sm.Ack(*evt.Request)
		slog.Info("slash command", "cmd", cmd.Command, "text", cmd.Text, "user", cmd.UserID)
		go s.handler.HandleSlashCommand(ctx, cmd)

	case socketmode.EventTypeEventsAPI:
		go s.handler.HandleEventsAPI(ctx, s.sm, evt)

	default:
		slog.Debug("unhandled event", "type", evt.Type)
	}
}
