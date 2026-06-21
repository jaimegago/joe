package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/paths"
)

// runIncidentCommand implements `joe incident <status|declare|resolve|list>` —
// the operator-facing surface for the incident regime (OPERATOR_SURFACE_-
// VERIFICATION.md items 5/6: enforcement on both agentic paths exists, but
// the only way to enter/leave incident mode was a hand-crafted authenticated
// POST to /regime/declare). This subcommand is the CLI trigger.
//
// status/declare/resolve hit the HTTP API (GET /api/v1/regime,
// POST /api/v1/regime/declare, POST /api/v1/regime/resolve) via the same
// loopback-keyed client the panic/unlock/review commands use. Declare/resolve
// authorize server-side against the regime-control zone (NOT the admin
// capability — D-0010/D-0012); the declaring/resolving principal is resolved
// from the credential, so the CLI does not pass one.
//
// `list` is intentionally a stub: there is no /regime/history endpoint
// (verified ABSENT — the durable record lives in the append-only audit_log,
// reachable only via the audit layer), so the subcommand reports that
// limitation honestly and exits non-zero rather than implying success.
func runIncidentCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe incident <status|declare|resolve|list> [flags]")
		fmt.Fprintln(stderr, "  status                      Show whether an incident regime is active (and who declared it).")
		fmt.Fprintln(stderr, "  declare --session <id> [--kind <kind>] [--reason <reason>]")
		fmt.Fprintln(stderr, "                              Declare an incident regime by promoting the named session")
		fmt.Fprintln(stderr, "                              in place. --kind defaults to \"human\" (\"joe\" is an inert")
		fmt.Fprintln(stderr, "                              seam the server refuses).")
		fmt.Fprintln(stderr, "  resolve [--reason <reason>] Resolve the active incident regime back to normal.")
		fmt.Fprintln(stderr, "  list                        (Unsupported in v1) Incident history lives in the audit log.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	// `list` needs no server connection — handle it before loading config /
	// opening a client so the v1 limitation is reported even with no daemon.
	if args[0] == "list" {
		fmt.Fprintln(stdout, "Incident history is queried via the audit log; see docs (no /regime/history endpoint in v1).")
		return 2
	}

	cfg, err := deps.loadConfig(paths.DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}
	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	joecoreURL := scheme + "://" + cfg.Server.Address
	var clientOpts []client.ClientOption
	if key := cfg.Server.LoopbackKey(); key != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(key))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	switch args[0] {
	case "status":
		return runIncidentStatus(ctx, args[1:], stdout, stderr, c)
	case "declare":
		return runIncidentDeclare(ctx, args[1:], stdout, stderr, c)
	case "resolve":
		return runIncidentResolve(ctx, args[1:], stdout, stderr, c)
	default:
		fmt.Fprintf(stderr, "Unknown incident subcommand: %s\n\n", args[0])
		usage()
		return 2
	}
}

func runIncidentStatus(ctx context.Context, args []string, stdout, stderr io.Writer, c *client.Client) int {
	fs := flag.NewFlagSet("joe incident status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	reg, err := c.GetRegime(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if reg.IsIncident() {
		fmt.Fprintf(stdout, "INCIDENT MODE ACTIVE — declared by %s at %s, kind: %s\n",
			derefOr(reg.DeclaredByPrincipal, "unknown"),
			formatRegimeTime(reg.DeclaredAt),
			derefOr(reg.DeclaredKind, "unknown"))
		return 0
	}
	fmt.Fprintln(stdout, "System operating normally (no incident declared).")
	return 0
}

func runIncidentDeclare(ctx context.Context, args []string, stdout, stderr io.Writer, c *client.Client) int {
	fs := flag.NewFlagSet("joe incident declare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	session := fs.String("session", "", "id of the existing session to promote in place to the incident master (required)")
	kind := fs.String("kind", "human", "regime declared_kind (\"human\"; \"joe\" is an inert Phase 1 seam)")
	reason := fs.String("reason", "", "optional free-text justification for the declaration")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Declaration is promote-in-place (§12.3): it promotes an existing
	// session rather than minting a fresh one, so a session id is required.
	if *session == "" {
		fmt.Fprintln(stderr, "Error: --session is required (incident declaration promotes an existing session in place).")
		return 2
	}

	res, err := c.DeclareIncident(ctx, *session, *kind, *reason)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// The declare response carries who declared it but no timestamp; read the
	// regime back so the confirmation can show when (best-effort — a declared
	// incident is the real outcome even if the follow-up read fails).
	when := "just now"
	if reg, regErr := c.GetRegime(ctx); regErr == nil && reg.DeclaredAt != nil {
		when = formatRegimeTime(reg.DeclaredAt)
	}
	fmt.Fprintf(stdout, "Incident declared by %s at %s (kind: %s).\n", res.DeclaredBy, when, *kind)
	if *reason != "" {
		fmt.Fprintf(stdout, "Reason: %s\n", *reason)
	}
	return 0
}

func runIncidentResolve(ctx context.Context, args []string, stdout, stderr io.Writer, c *client.Client) int {
	fs := flag.NewFlagSet("joe incident resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "optional free-text justification for the resolution")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	res, err := c.ResolveIncident(ctx, *reason)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Incident resolved by %s. System operating normally.\n", res.ResolvedBy)
	if *reason != "" {
		fmt.Fprintf(stdout, "Reason: %s\n", *reason)
	}
	return 0
}

// derefOr returns *p, or fallback when p is nil.
func derefOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// formatRegimeTime renders a regime timestamp in RFC3339, or a placeholder
// when the server omitted it.
func formatRegimeTime(t *time.Time) string {
	if t == nil {
		return "unknown time"
	}
	return t.UTC().Format(time.RFC3339)
}
