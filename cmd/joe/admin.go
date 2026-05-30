package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/rbac"
)

// runAdminCommand implements `joe admin <list|grant|revoke>` — the Phase H
// operator surface for inspecting and managing dynamic admin status (see
// docs/joe-identity-design.md §2.9 and docs/DECISIONS.md D-0011).
//
// Admin status is a principal-scoped capability stored in admin_principals
// (migration 016) and consulted by the policy engine at decision time. The
// `joe zone` command provisions per-zone grants in rbac_policies; `joe admin`
// provisions admin rows. They are deliberately separate surfaces because
// admin and zone authority have different semantics: admin bypasses the
// per-principal grant requirement on every zone (including zones created
// after designation), while zone grants are scoped to one zone at a time.
//
// The configured auth.admin_email bootstrap path continues to work
// regardless of this CLI: it writes the same admin_principals row through
// the auth.Provisioner on every matching login. The CLI exists so an
// operator can delegate additional admins (consistent with how Phase C
// added `joe zone`) without restarting joe-core or editing config.
func runAdminCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe admin <list|grant|revoke> [flags]")
		fmt.Fprintln(stderr, "  list                              List all dynamic admins (admin_principals rows).")
		fmt.Fprintln(stderr, "  grant  --principal <user:|svc:> [--reason ...]")
		fmt.Fprintln(stderr, "                                    Mark a principal as admin (idempotent). Admin allows any")
		fmt.Fprintln(stderr, "                                    zone+action the zone itself permits, on zones present now")
		fmt.Fprintln(stderr, "                                    or created later.")
		fmt.Fprintln(stderr, "  revoke --principal <user:|svc:>   Remove a principal's admin status.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	cfg, err := deps.loadConfig(paths.DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	repo, closeRepo, err := deps.openRBACRepo(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer func() { _ = closeRepo() }()

	switch args[0] {
	case "grant":
		return runAdminGrant(ctx, args[1:], stdout, stderr, repo)
	case "revoke":
		return runAdminRevoke(ctx, args[1:], stdout, stderr, repo)
	case "list":
		return runAdminList(ctx, args[1:], stdout, stderr, repo)
	default:
		fmt.Fprintf(stderr, "Unknown admin subcommand: %s\n\n", args[0])
		usage()
		return 2
	}
}

// runAdminGrant marks a principal as admin. The configured admin_email
// bootstrap path is preserved (it writes through the same AddAdmin call on
// every matching login); this CLI is the operator-on-host path for
// promoting additional admins.
//
// On promotion, ALL rbac_policies rows for the principal are also removed:
// admin authority subsumes per-zone grants, so leaving them in place would
// give the principal two storage sites for the same authority. The cleanup
// is the same one auth.Provisioner.GrantAdmin performs for the bootstrap
// path — single source of truth holds whichever way the principal becomes
// admin (D-0011).
func runAdminGrant(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe admin grant", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "principal to promote (user:<email> or svc:<name>) (required)")
	reason := fs.String("reason", "", "optional free-text justification recorded with the grant")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *principal == "" {
		fmt.Fprintln(stderr, "Error: --principal is required")
		return 1
	}
	if !validatePrincipal(*principal, stderr) {
		return 1
	}

	already, err := repo.IsAdmin(ctx, *principal)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if err := repo.AddAdmin(ctx, rbac.Admin{
		Principal: *principal,
		GrantedAt: time.Now().UTC(),
		GrantedBy: "cli",
		Reason:    *reason,
	}); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Single source of truth: drop any per-zone rbac_policies rows for
	// this principal (now redundant — admin allows everything the zones
	// themselves permit). Same cleanup the bootstrap path performs (see
	// auth.Provisioner.GrantAdmin). Matches the static-structural
	// assertion in the Phase H tests.
	existing, err := repo.ListPoliciesForPrincipal(ctx, *principal)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	for _, p := range existing {
		if _, err := repo.DeletePolicyForPrincipalZone(ctx, p.Principal, p.ZoneID); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}

	if already {
		fmt.Fprintf(stdout, "Principal %q is already admin (granted_at/reason updated).\n", *principal)
	} else {
		fmt.Fprintf(stdout, "Granted admin status to %q.\n", *principal)
	}
	return 0
}

// runAdminRevoke removes a principal's admin status. The configured
// admin_email path will re-grant on next matching login (the bootstrap
// path is idempotent), so an operator who wants permanent revocation must
// also clear auth.admin_email from config — documented in D-0011.
func runAdminRevoke(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe admin revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "principal to demote (user:<email> or svc:<name>) (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *principal == "" {
		fmt.Fprintln(stderr, "Error: --principal is required")
		return 1
	}

	n, err := repo.RemoveAdmin(ctx, *principal)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintf(stdout, "%q is not admin (nothing to revoke).\n", *principal)
		return 0
	}
	fmt.Fprintf(stdout, "Revoked admin status from %q.\n", *principal)
	return 0
}

func runAdminList(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe admin list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	admins, err := repo.ListAdmins(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if len(admins) == 0 {
		fmt.Fprintln(stdout, "No admin principals found.")
		return 0
	}
	fmt.Fprintf(stdout, "%-40s  %-25s  %s\n", "PRINCIPAL", "GRANTED_BY", "GRANTED_AT")
	for _, a := range admins {
		fmt.Fprintf(stdout, "%-40s  %-25s  %s\n", a.Principal, a.GrantedBy, a.GrantedAt.Format(time.RFC3339))
	}
	return 0
}
