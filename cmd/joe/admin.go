package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// adminGrantStore is the narrow surface `joe admin bootstrap` needs: the
// one-shot first-admin grant. It exists so routing and refusal tests can inject
// a fake without opening a real database, mirroring panicRowStore for
// `joe unlock` and backupStore for `joe db backup`.
//
// The surface is deliberately this small. The command has no reason to read the
// admin roster, list principals, or write anything else, and a wider seam would
// let it acquire one.
type adminGrantStore interface {
	GrantFirstAdmin(ctx context.Context, principal rbac.Principal) (bool, error)
}

// defaultOpenAdminStore opens the database `joe admin bootstrap` writes to and
// returns the grant seam with a closer.
//
// It takes the config the command already loaded rather than loading its own.
// The command resolves the operator's principal against server.service_accounts
// from that config; resolving the database from a second, independently-loaded
// config would let `--config` redirect one and not the other, and a command that
// validates a principal against one configuration while writing to another
// database is worse than having no flag at all.
//
// It DOES run migrations, following defaultOpenPanicStore rather than
// defaultOpenBackupStore. The two offline precedents diverge for a reason that
// resolves cleanly here: backup declines to migrate because its promise is a
// copy and upgrading the operator's database as a side effect would break that
// promise, most damagingly for the operator backing up a database they already
// suspect is damaged. This command promises a write, and the tables it writes
// (admin_principals, audit_log) must exist for it to keep that promise — on a
// fresh install the operator may reasonably run it before Joe has ever booted.
// Migrating is what `joe unlock` does, for the same reason: mirror boot so the
// rows the command acts on are there.
//
// The repository is built with NewRepositoryWithAudit, NOT NewRepository. That
// is the whole of the audit answer: the grant and its admin.grant row commit in
// one transaction, the same guarantee the daemon has. The un-audited
// constructor stays test-only, as its doc comment claims.
//
// There is NO occupancy refusal, deliberately, and no daemon contact. `joe db
// restore` refuses a live daemon because it replaces the database file
// wholesale; this command performs one small transactional write, which is
// exactly the shape WAL plus busy_timeout supports alongside a running daemon —
// the same argument `joe unlock` and `joe db backup` already make for opening
// the live database. A running daemon picks the grant up with no restart, since
// the policy engine reads admin status at decision time
// (internal/rbac/policy.go) rather than caching a boot snapshot.
func defaultOpenAdminStore(cfg *config.Config) (adminGrantStore, func() error, error) {
	dbCfg, err := databaseConfigFor(cfg)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.New(dbCfg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("ensure schema: %w", err)
	}
	auditRepo := audit.NewRepository(s.DB(), s.Driver())
	rbacRepo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	return auth.NewProvisioner(rbacRepo), s.Close, nil
}

// runAdminCommand implements `joe admin <bootstrap>` — operator utilities that
// act on Joe's admin roster in the local database rather than through the
// running daemon. The namespace is deliberately open-ended, like `joe db`.
func runAdminCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: joe admin <bootstrap> [flags]")
		fmt.Fprintln(w, "  bootstrap <principal> [--config <path>]")
		fmt.Fprintln(w, "                              Grant admin to a configured service account, once, on a")
		fmt.Fprintln(w, "                              database that has no admin yet. Refused if any admin")
		fmt.Fprintln(w, "                              exists; every later grant goes through the admin API.")
		fmt.Fprintln(w, "                              --config names the same config file the daemon is")
		fmt.Fprintln(w, "                              started with; without it, ~/.joe/config.yaml is used.")
	}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	if isHelpToken(args[0]) {
		usage(stdout)
		return 0
	}

	switch args[0] {
	case "bootstrap":
		return runAdminBootstrap(ctx, args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "Unknown admin subcommand: %s\n\n", args[0])
		usage(stderr)
		return 2
	}
}

// runAdminBootstrap implements `joe admin bootstrap <principal>`: the offline
// first-admin grant.
//
// The config it reads is the default ~/.joe/config.yaml unless --config names
// another, mirroring the daemon's flag so an operator can reuse their `joe
// --config ...` invocation verbatim. One config load serves both uses: the
// service-account set the principal is validated against, and the database the
// grant is written to. See defaultOpenAdminStore.
//
// It exists because a deployment configured with service accounts and no
// identity provider cannot mint an admin at all. The OIDC login and callback
// routes are not registered, so the admin_email bootstrap writer is not merely
// unconfigured but structurally absent; RBAC is enabled, so requireAdmin
// genuinely enforces on the admin REST surface; and the last-admin guard stops
// the roster reaching zero from one. Zero admins is an absorbing state, and
// this command is the only way out of it.
//
// Two restrictions keep that from being a general-purpose privilege escalator.
// It accepts only principals that resolve to a CONFIGURED service account, and
// it is refused the moment any admin exists — with no --force and no override,
// which is the point rather than an omission. Every grant after the first is
// forced through the governed, audited REST surface.
func runAdminBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe admin bootstrap", flag.ContinueOnError)
	// Mirrors the daemon's --config exactly: same name, same meaning, a
	// relative value taken verbatim against the working directory. An operator
	// who starts Joe with `joe --config ./deploy/joe.yaml` must be able to reuse
	// that token here, because this command reads the SAME two things out of it
	// the daemon does — server.service_accounts, which decides whether the named
	// principal exists, and database.driver/dsn, which decides where the grant
	// lands. Resolving the default path while the daemon runs on another file
	// silently validates against one install and writes to another.
	//
	// Empty default rather than paths.DefaultConfigPath() so "not passed" stays
	// distinguishable from "passed": the two take different missing-file
	// postures below.
	configPath := fs.String("config", "", "path to the config file (default ~/.joe/config.yaml)")
	// --config takes a following token, and an operator will naturally write the
	// principal first. reorderFlagsFirst is the tree's existing answer to the
	// stdlib flag package's flags-before-positionals rule.
	if code, ok := parseCLIFlags(fs, reorderFlagsFirst(args, map[string]bool{"config": true}), stdout, stderr); !ok {
		return code
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Error: bootstrap requires exactly one <principal> argument.")
		fmt.Fprintln(stderr, "Usage: joe admin bootstrap <principal> [--config <path>]")
		fmt.Fprintln(stderr, "The principal names a service account from server.service_accounts, as")
		fmt.Fprintln(stderr, "either svc:<name> or the bare <name>.")
		return 2
	}
	arg := strings.TrimSpace(fs.Arg(0))

	// The asymmetry this command established is now shared by every --config in
	// the CLI, so it lives in resolveConfigFlag rather than here. Its reasoning
	// applies unchanged: a missing file at the DEFAULT path is not an error —
	// config.Load falls back to defaults, and the command then refuses with "no
	// service accounts are configured", which is the accurate diagnosis for an
	// install that has none. A missing file at an EXPLICITLY NAMED path is an
	// operational failure: silently falling back to defaults would resolve the
	// principal against an empty account set and, worse, target the default
	// database instead of the one that config names.
	cfgPath, ok := resolveConfigFlag(*configPath, stderr)
	if !ok {
		return 1
	}

	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	principal, code := resolveBootstrapPrincipal(arg, cfg, stderr)
	if code != 0 {
		return code
	}

	gs, closeStore, err := deps.openAdminStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to open database: %v\n", err)
		fmt.Fprintln(stderr, "Check that the account running this command can read Joe's database and config.")
		return 1
	}
	defer func() { _ = closeStore() }()

	granted, err := gs.GrantFirstAdmin(ctx, principal)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to grant admin: %v\n", err)
		return 1
	}
	if !granted {
		// The containment clause. Report it as the refusal it is, and name the
		// surface that owns every subsequent grant.
		fmt.Fprintln(stderr, "Error: this database already has an admin; refusing to grant another.")
		fmt.Fprintln(stderr, "This command exists only to open a database that has no admin at all. Once one")
		fmt.Fprintln(stderr, "exists, further admins are granted through the admin API")
		fmt.Fprintln(stderr, "(POST /api/v1/admin/admins), which is gated and audited. There is no override.")
		return 1
	}

	fmt.Fprintf(stdout, "Granted admin to %s.\n", principal)
	fmt.Fprintln(stdout, "This was a one-time bootstrap: with an admin now present, this command refuses to")
	fmt.Fprintln(stdout, "run again on this database. Grant any further admins through the admin API.")
	fmt.Fprintln(stdout, "A running Joe picks this up without a restart.")
	return 0
}

// resolveBootstrapPrincipal turns the operator's argument into the principal to
// grant, or explains the refusal and returns a non-zero exit code.
//
// The argument may be svc:<name> or the bare <name>; both resolve against
// server.service_accounts. Human identities are refused outright rather than
// looked up, because the refusal has a different cause and a different remedy
// than "no such account".
//
// The principal is minted through rbac.ServicePrincipal — the same single point
// that mints it for the authenticating request path
// (auth.NewServiceAccountResolver) — so the string written to admin_principals
// is provably the one a presented key resolves to. Deriving it here by string
// concatenation instead is how the granted row and the authenticated principal
// would come to disagree.
func resolveBootstrapPrincipal(arg string, cfg *config.Config, stderr io.Writer) (rbac.Principal, int) {
	if arg == "" {
		fmt.Fprintln(stderr, "Error: the principal is empty.")
		return "", 2
	}

	// Human identities: refused on their own terms, before any lookup.
	//
	// Two independent reasons, and the operator needs both. In a deployment
	// with no identity provider a human principal can never authenticate, so
	// the grant writes an inert row that arms later — for whoever first
	// presents that identifier, whenever an IdP is eventually configured. And a
	// deployment that HAS an identity provider already has a bootstrap path
	// (auth.admin_email) and does not need this one.
	lower := strings.ToLower(arg)
	if strings.HasPrefix(lower, rbac.PrefixUser) || strings.HasPrefix(lower, rbac.PrefixGroup) {
		fmt.Fprintf(stderr, "Error: %s is a human identity; this command grants admin to service accounts only.\n", arg)
		fmt.Fprintln(stderr, "Without an identity provider configured, a human principal can never authenticate,")
		fmt.Fprintln(stderr, "so the grant would sit inert until one is — and then arm for whoever first presents")
		fmt.Fprintln(stderr, "that identifier. With an identity provider configured, set auth.admin_email instead:")
		fmt.Fprintln(stderr, "that principal is granted admin on its next login, which is the bootstrap path for")
		fmt.Fprintln(stderr, "human admins.")
		return "", 1
	}

	name := strings.TrimPrefix(arg, rbac.PrefixSvc)
	if len(cfg.Server.ServiceAccounts) == 0 {
		fmt.Fprintln(stderr, "Error: no service accounts are configured, so there is no principal to grant admin to.")
		fmt.Fprintln(stderr, "Add one under server.service_accounts in Joe's config, then re-run.")
		fmt.Fprintln(stderr, "Configure a DEDICATED account for administration rather than naming the shared")
		fmt.Fprintln(stderr, "general-purpose key here — otherwise admin rides on the bearer secret every caller")
		fmt.Fprintln(stderr, "already holds.")
		return "", 1
	}

	var configured []string
	for _, sa := range cfg.Server.ServiceAccounts {
		p, err := rbac.ServicePrincipal(sa.Name)
		if err != nil {
			// A config this invalid is fatal at boot too; say so rather than
			// silently skipping the entry and reporting "no such account".
			fmt.Fprintf(stderr, "Error: service account %q in the config is invalid: %v\n", sa.Name, err)
			return "", 1
		}
		configured = append(configured, string(p))
		if sa.Name == name {
			return p, 0
		}
	}

	fmt.Fprintf(stderr, "Error: %q does not name a configured service account.\n", arg)
	fmt.Fprintf(stderr, "Configured service accounts: %s\n", strings.Join(configured, ", "))
	fmt.Fprintln(stderr, "Name one of those, as either svc:<name> or the bare <name>. Prefer a DEDICATED")
	fmt.Fprintln(stderr, "administration account over the shared general-purpose key, so admin does not ride")
	fmt.Fprintln(stderr, "on the bearer secret every caller already holds.")
	return "", 1
}
