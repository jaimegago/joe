// joe-slack is a Slack bot that connects Joe's infrastructure intelligence
// to Slack channels via Socket Mode (no public URL required).
//
// Environment variables:
//
//	SLACK_BOT_TOKEN  — Bot User OAuth token (xoxb-...)
//	SLACK_APP_TOKEN  — App-Level token with connections:write scope (xapp-...)
//	JOE_SERVER       — joecored base URL (default: http://localhost:7777)
//	JOE_API_KEY      — Bearer token for joecored API auth (optional)
//
// Slash commands exposed:
//
//	/joe ask <query>  — search the infrastructure graph and knowledge store
//	/joe status       — show graph summary
//	/joe incidents    — show active incidents (requires Alertmanager source)
//	/joe help         — show available commands
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jaimegago/joe/internal/client"
	jslack "github.com/jaimegago/joe/internal/slack"
	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("joe-slack: SLACK_BOT_TOKEN is required")
	}
	appToken := os.Getenv("SLACK_APP_TOKEN")
	if appToken == "" {
		log.Fatal("joe-slack: SLACK_APP_TOKEN is required (xapp-...)")
	}

	serverURL := os.Getenv("JOE_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:7777"
	}
	apiKey := os.Getenv("JOE_API_KEY")

	var clientOpts []client.ClientOption
	if apiKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(apiKey))
	}
	coreClient := client.New(serverURL, clientOpts...)

	api := gslack.New(botToken, gslack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)

	agent := jslack.NewAgent(coreClient)
	srv := jslack.NewServer(api, sm, agent)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("joe-slack: starting", "server", serverURL)
	fmt.Fprintf(os.Stderr, "joe-slack: connecting to joecored at %s\n", serverURL)

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("joe-slack: %v", err)
	}
}
