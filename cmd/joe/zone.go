package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// rbacRepo is the union of the RBAC surface the `joe zone` and `joe admin`
// commands need. It is satisfied by *rbac.SQLRepository (and by the broader
// rbac.Repository interface); declaring the narrow shape here lets tests
// inject an in-memory repo without opening a database. Phase H expands the
// shape with admin-status methods so `joe admin` can read and manage rows
// in the admin_principals table introduced by migration 016.
type rbacRepo interface {
	// Zones + policies (Phase C, used by `joe zone`).
	GetZone(ctx context.Context, id string) (*rbac.Zone, error)
	ListZones(ctx context.Context) ([]rbac.Zone, error)
	CreatePolicy(ctx context.Context, p rbac.Policy) (*rbac.Policy, error)
	DeletePolicyForPrincipalZone(ctx context.Context, principal, zoneID string) (int64, error)
	ListPolicies(ctx context.Context) ([]rbac.Policy, error)
	ListPoliciesForPrincipal(ctx context.Context, principal string) ([]rbac.Policy, error)

	// Admin status (Phase H, used by `joe admin`, D-0011).
	IsAdmin(ctx context.Context, principal string) (bool, error)
	ListAdmins(ctx context.Context) ([]rbac.Admin, error)
	AddAdmin(ctx context.Context, a rbac.Admin) error
	RemoveAdmin(ctx context.Context, principal string) (int64, error)
}

// openRBACRepoDefault opens the joe server database directly and returns an RBAC
// repository over it. Zone provisioning is an operator-on-host task (design
// §2.9: "CLI command only for v1"); it writes rbac_policies rows directly
// rather than through an admin HTTP endpoint (deliberately none in this phase),
// which also sidesteps the bootstrap chicken-and-egg (you do not need an
// already-authorized session to grant the first one). It must run on the host
// that owns the SQLite database. The returned closer must be called by the
// caller.
func openRBACRepoDefault(cfg *config.Config) (rbacRepo, func() error, error) {
	dbPath, err := paths.DatabasePath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve database path: %w", err)
	}
	dbCfg := store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dbPath}
	if cfg.Database.Driver != "" {
		dbCfg.Driver = cfg.Database.Driver
	}
	if cfg.Database.DSN != "" {
		dbCfg.DSN = cfg.Database.DSN
	}
	sqlStore, err := store.New(dbCfg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	// Idempotent — the joe server normally created the schema already; running here
	// covers the rare case where provisioning happens before the daemon's first
	// boot.
	if err := sqlStore.Migrate(); err != nil {
		_ = sqlStore.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}
	repo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	return repo, sqlStore.Close, nil
}

// runZoneCommand implements `joe zone <grant|revoke|list>` — the Phase C
// operator surface for provisioning RBAC authority to user:/svc: principals
// (design §2.9). It writes/removes rows in rbac_policies (principal → zone
// grants). Source→zone assignment continues to use the existing admin API and
// is out of scope here.
func runZoneCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe zone <grant|revoke|list> [flags]")
		fmt.Fprintln(stderr, "  grant  --principal <user:email|svc:name> --zone <zone-id>")
		fmt.Fprintln(stderr, "                              Grant a principal access to a security zone (writes an rbac_policies row).")
		fmt.Fprintln(stderr, "  revoke --principal <user:email|svc:name> --zone <zone-id>")
		fmt.Fprintln(stderr, "                              Remove a principal's grant on a zone (deletes the rbac_policies row).")
		fmt.Fprintln(stderr, "  list   [--principal <user:email|svc:name>]")
		fmt.Fprintln(stderr, "                              List all policies, or just one principal's grants.")
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
		return runZoneGrant(ctx, args[1:], stdout, stderr, repo)
	case "revoke":
		return runZoneRevoke(ctx, args[1:], stdout, stderr, repo)
	case "list":
		return runZoneList(ctx, args[1:], stdout, stderr, repo)
	default:
		fmt.Fprintf(stderr, "Unknown zone subcommand: %s\n\n", args[0])
		usage()
		return 2
	}
}

// validatePrincipal rejects a principal that does not carry a reserved kind
// prefix. Provisioning targets only user: and svc: principals (group: is a v2
// seam with nothing minting it); an unprefixed string would be a typo that
// silently grants nobody.
func validatePrincipal(principal string, stderr io.Writer) bool {
	if !rbac.HasReservedPrefix(principal) {
		fmt.Fprintf(stderr, "Error: principal %q must carry a reserved prefix (%q, %q, or %q)\n",
			principal, rbac.PrefixUser, rbac.PrefixGroup, rbac.PrefixSvc)
		return false
	}
	return true
}

func runZoneGrant(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe zone grant", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "principal to grant (user:<email> or svc:<name>) (required)")
	zone := fs.String("zone", "", "security zone id (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *principal == "" || *zone == "" {
		fmt.Fprintln(stderr, "Error: --principal and --zone are required")
		return 1
	}
	if !validatePrincipal(*principal, stderr) {
		return 1
	}

	// Reject unknown zones up front so a typo fails loudly rather than creating
	// a grant that gates on a non-existent zone.
	z, err := repo.GetZone(ctx, *zone)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if z == nil {
		fmt.Fprintf(stderr, "Error: zone %q does not exist (run `joe zone list` or create it first)\n", *zone)
		return 1
	}

	// Idempotent grant: if the (principal, zone) policy already exists, report
	// it rather than erroring on the UNIQUE constraint.
	existing, err := repo.ListPoliciesForPrincipal(ctx, *principal)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	for _, p := range existing {
		if p.ZoneID == *zone {
			fmt.Fprintf(stdout, "Principal %q already has zone %q.\n", *principal, *zone)
			return 0
		}
	}

	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: *principal, ZoneID: *zone}); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Granted %q access to zone %q.\n", *principal, *zone)
	return 0
}

func runZoneRevoke(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe zone revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "principal to revoke (user:<email> or svc:<name>) (required)")
	zone := fs.String("zone", "", "security zone id (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *principal == "" || *zone == "" {
		fmt.Fprintln(stderr, "Error: --principal and --zone are required")
		return 1
	}

	n, err := repo.DeletePolicyForPrincipalZone(ctx, *principal, *zone)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintf(stdout, "No grant found for %q on zone %q (nothing to revoke).\n", *principal, *zone)
		return 0
	}
	fmt.Fprintf(stdout, "Revoked %q access to zone %q.\n", *principal, *zone)
	return 0
}

func runZoneList(ctx context.Context, args []string, stdout, stderr io.Writer, repo rbacRepo) int {
	fs := flag.NewFlagSet("joe zone list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "filter to a single principal")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var policies []rbac.Policy
	var err error
	if *principal != "" {
		policies, err = repo.ListPoliciesForPrincipal(ctx, *principal)
	} else {
		policies, err = repo.ListPolicies(ctx)
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if len(policies) == 0 {
		fmt.Fprintln(stdout, "No zone grants found.")
		return 0
	}
	fmt.Fprintf(stdout, "%-40s  %s\n", "PRINCIPAL", "ZONE")
	for _, p := range policies {
		fmt.Fprintf(stdout, "%-40s  %s\n", p.Principal, p.ZoneID)
	}
	return 0
}
