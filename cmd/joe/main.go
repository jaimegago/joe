package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	mcpserver "github.com/mark3labs/mcp-go/server"
	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/mcp"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/skills"
	jslack "github.com/jaimegago/joe/internal/slack"
	"github.com/jaimegago/joe/internal/store"
)

// panicRowStore is the narrow surface the `joe unlock` CLI needs to operate on
// the single panic DB row. It exists so tests can inject a fake without opening
// a real database.
type panicRowStore interface {
	IsPanicked(ctx context.Context) (bool, error)
	ClearPanicked(ctx context.Context) error
}

type runDeps struct {
	loadConfig       func(path string) (*config.Config, error)
	joeDirPath       func() (string, error)
	newClient        func(baseURL string, opts ...client.ClientOption) *client.Client
	newSkillManager  func(root string, trusted []string, policy *skills.Policy) skillManager
	loadSkillsPolicy func(joeDir string) (*skills.Policy, error)
	// runServer boots the HTTP API daemon — Joe's default (no-subcommand)
	// behavior. Injectable so dispatcher tests can assert routing without
	// actually binding a port or opening a database.
	runServer func(ctx context.Context) int
	// openPanicStore opens the panic DB row store for `joe unlock`, returning the
	// store, a closer, and any error. It opens the database directly (never
	// contacting a running process) so recovery works while Joe is down after a
	// panic exit. Injectable so unlock tests can supply a fake row.
	//
	// Like openAdminStore below it takes the ALREADY-LOADED config rather than
	// resolving its own, so `--config` cannot redirect the command's config
	// reading and its database targeting independently.
	openPanicStore func(cfg *config.Config) (panicRowStore, func() error, error)
	// openBackupStore opens the database `joe db backup` copies from, returning
	// it, a closer, and any error. Like openPanicStore it opens the file directly
	// rather than contacting the daemon, and takes the already-loaded config.
	// Injectable so the backup command's routing and error-path tests need no
	// real database.
	openBackupStore func(cfg *config.Config) (backupStore, func() error, error)
	// resolveDatabaseConfig reports the driver and DSN the daemon would use, so
	// `joe db restore` knows which file it is replacing WITHOUT opening it —
	// opening the target through store.New would create the sidecars restore
	// exists to remove. Injectable so tests can target a temp directory.
	//
	// It takes the config restore already loaded, which is what makes restore's
	// two config-governed uses coherent: the database it replaces and the
	// encryption key it checks for come from the SAME config object, so a
	// caller-selected config path redirects both or neither.
	resolveDatabaseConfig func(cfg *config.Config) (store.DatabaseConfig, error)
	// openSourceDB opens a database file READ-ONLY, for restore's pre-flight
	// checks and for the copy itself. Injectable so refusal tests need no real
	// database.
	openSourceDB func(path string) (sourceDB, func() error, error)
	// encryptionKeyPath reports where the component-config encryption key lives,
	// honouring database.encryption_key_path so restore checks the same file the
	// daemon would load. It takes the same config object resolveDatabaseConfig
	// does — see there. Injectable so tests can control whether a key appears
	// present.
	encryptionKeyPath func(cfg *config.Config) (string, error)
	// probeTargetOccupied reports whether another process holds the database at
	// path open — the discriminator between a running daemon and an unclean
	// shutdown, which look identical on disk. Injectable so tests can force
	// either answer without spawning a daemon.
	probeTargetOccupied func(path string) (bool, error)
	// getenv reads process environment. `joe mcp` and `joe slack` take their
	// entire configuration this way rather than through flags or the config
	// file, so this is the seam that makes their refusal paths testable without
	// mutating the real environment.
	getenv func(name string) string
	// serveMCP runs the MCP stdio server, blocking until it errors or stdin
	// closes. Injectable because every path through `joe mcp` reaches it — with
	// no early return, a test could not otherwise observe the command at all.
	serveMCP func(s *mcpserver.MCPServer) error
	// openAdminStore opens the first-admin grant seam for `joe admin bootstrap`.
	// Like the other offline opens it goes to the database file directly rather
	// than contacting the daemon. Injectable so the command's routing and
	// refusal tests need no real database — which matters here because
	// paths.JoeDirPath does NOT honour $HOME, so a test cannot isolate itself
	// by pointing the home directory elsewhere.
	//
	// It takes the ALREADY-LOADED config rather than resolving its own. That is
	// what makes `--config` coherent: the config that validated the principal is
	// the same object that names the database the grant lands in, so the two
	// cannot be redirected independently.
	openAdminStore func(cfg *config.Config) (adminGrantStore, func() error, error)
}

func defaultRunDeps() runDeps {
	return runDeps{
		loadConfig: config.Load,
		joeDirPath: paths.JoeDirPath,
		newClient:  client.New,
		newSkillManager: func(root string, trusted []string, policy *skills.Policy) skillManager {
			return skills.NewManager(root, nil).
				WithTrustedSources(trusted).
				WithPolicy(policy)
		},
		loadSkillsPolicy: func(joeDir string) (*skills.Policy, error) {
			return skills.LoadPolicy(joeDir)
		},
		runServer:             runServer,
		openPanicStore:        defaultOpenPanicStore,
		openBackupStore:       defaultOpenBackupStore,
		resolveDatabaseConfig: databaseConfigFor,
		openSourceDB:          defaultOpenSourceDB,
		encryptionKeyPath:     encryptionKeyPathFor,
		probeTargetOccupied:   defaultProbeTargetOccupied,
		getenv:                os.Getenv,
		serveMCP: func(s *mcpserver.MCPServer) error {
			return mcpserver.ServeStdio(s)
		},
		openAdminStore: defaultOpenAdminStore,
	}
}

// defaultOpenPanicStore opens the SQLite store directly and returns its panic-row
// store plus a closer. It honors the same database config the daemon uses
// (cfg.Database overrides, else ~/.joe/joe.db) and runs migrations so the panic
// row exists, mirroring boot. It NEVER contacts a running process: after a panic
// the daemon has already exited (os.Exit), and in the not-panicked case the
// command only reads the row, so a brief shared SQLite open (WAL + busy_timeout)
// does not disrupt a healthy running daemon.
//
// The config is passed in rather than loaded here, so that `joe unlock --config`
// names the database the command acts on. The cfg.Database override block below
// is deliberately NOT folded into databaseConfigFor: that fold is one half of
// the refactor docs/backlog/admin-bootstrap-cli-04.md holds for its own
// decision, and doing half of it opportunistically is what that item exists to
// prevent.
func defaultOpenPanicStore(cfg *config.Config) (panicRowStore, func() error, error) {
	dbPath, err := paths.DatabasePath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve database path: %w", err)
	}
	dbCfg := store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dbPath}
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Database.Driver != "" {
		dbCfg.Driver = cfg.Database.Driver
	}
	if cfg.Database.DSN != "" {
		dbCfg.DSN = cfg.Database.DSN
	}
	s, err := store.New(dbCfg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("ensure schema: %w", err)
	}
	return s.PanicStore(), s.Close, nil
}

// skillManager is the narrow surface the `joe skills` CLI needs from the
// install manager. It exists so tests can inject a fake without spawning git.
type skillManager interface {
	Install(ctx context.Context, repo, ref, subdir string) (*skills.Install, error)
	Remove(ctx context.Context, name string, force bool) ([]string, error)
	Update(ctx context.Context, name string) ([]*skills.Install, error)
	List() ([]skills.Install, error)
	Approve(ctx context.Context, name string) (*skills.Install, error)
	Reject(ctx context.Context, name string) ([]string, error)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithDeps(ctx, args, stdout, stderr, defaultRunDeps())
}

// runPanicCommand sends an emergency shutdown request to the joe server.
func runPanicCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe panic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Empty default rather than paths.DefaultConfigPath(): the flag already
	// existed here, but with the default path as its default value it could not
	// tell "not passed" from "passed", so an explicitly-named path that does not
	// resolve fell back to defaults and triggered a shutdown against whichever
	// server the default config names. Distinguishing the two is what lets this
	// command take the same missing-file posture as every other --config.
	configPath := fs.String("config", "", "path to the config file (default ~/.joe/config.yaml)")
	reason := fs.String("reason", "operator triggered via CLI", "reason for the emergency shutdown")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "Error: panic takes no positional arguments")
		return 2
	}

	cfgPath, ok := resolveConfigFlag(*configPath, stderr)
	if !ok {
		return 1
	}

	cfg, err := deps.loadConfig(cfgPath)
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

	if err := c.TriggerPanic(ctx, *reason); err != nil {
		fmt.Fprintf(stderr, "Error: failed to trigger panic: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Emergency shutdown triggered. joe will restart in safe mode.")
	fmt.Fprintln(stdout, "Use 'joe unlock' to clear the panic state, then restart to resume normal operation.")
	return 0
}

// runUnlockCommand clears the single panic DB row as a deliberate operator
// acknowledgment (D-0018). It opens the database DIRECTLY and does NOT contact or
// signal any running process, does NOT lower any live write floor, and does NOT
// reference the floor value — it acts on the panic row only. Clearing takes
// effect on restart: a running Joe stays read-only because the floor was sealed
// at boot and is never re-derived mid-process.
//
// It is read-then-report-conditionally and idempotent: it reads the row first
// and only writes when a panic is present, so running it twice — or on a healthy
// Joe — does nothing harmful and reports accurately. The functional cases (panic
// present / absent) both exit 0; only a genuine store-access failure exits
// non-zero, so the report never lies about what happened.
func runUnlockCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe unlock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "optional acknowledgment reason recorded to the log")
	// --config names the database this command clears the panic row in, via
	// database.driver/dsn — the single thing configuration governs here. Without
	// it, an operator running Joe from another config file clears the panic row
	// in a database the daemon never reads, and is told the panic was cleared.
	configPath := fs.String("config", "", "path to the config file (default ~/.joe/config.yaml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "Error: unlock takes no positional arguments")
		return 2
	}

	cfgPath, ok := resolveConfigFlag(*configPath, stderr)
	if !ok {
		return 1
	}
	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	ps, closeStore, err := deps.openPanicStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to open panic store: %v\n", err)
		return 1
	}
	defer func() { _ = closeStore() }()

	panicked, err := ps.IsPanicked(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to read panic state: %v\n", err)
		return 1
	}

	if !panicked {
		// Nothing to clear. Do not imply a restart is needed or that writes will
		// resume — the CLI only knows the row, not the daemon's live state.
		fmt.Fprintln(stdout, "Joe is not in a panicked state; nothing to clear.")
		return 0
	}

	if err := ps.ClearPanicked(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: failed to clear panic state: %v\n", err)
		return 1
	}
	if *reason != "" {
		slog.Info("panic state acknowledged and cleared", "reason", *reason)
	}
	// Phrased against the panic row, not the daemon: another read-only posture
	// (observation mode) may independently hold the floor up, so writes are not
	// promised to resume unconditionally.
	fmt.Fprintln(stdout, "Panic state was present and has been cleared. Restart any running Joe to resume writes if no other read-only posture is set; writes remain blocked until restart.")
	return 0
}

// runMCPCommand starts Joe as an MCP stdio server.
// Connection details are read from environment variables:
//
//	JOE_SERVER  — joe server base URL (default: http://localhost:7777)
//	JOE_API_KEY — Bearer token for joe API auth (optional)
//
// It takes no flags and no positional arguments — configuration comes solely
// from the environment (D-0132 withholds --config here deliberately, since
// this command reads no config file) — so the flag set below exists only to
// catch and reject anything the operator passes, rather than silently
// ignoring it.
func runMCPCommand(_ context.Context, args []string, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "Error: mcp takes no positional arguments")
		return 2
	}

	serverURL := deps.getenv("JOE_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:7777"
	}
	apiKey := deps.getenv("JOE_API_KEY")

	var opts []client.ClientOption
	if apiKey != "" {
		opts = append(opts, client.WithAPIKey(apiKey))
	}

	coreClient := deps.newClient(serverURL, opts...)
	s := mcp.NewServer(coreClient)

	fmt.Fprintf(stderr, "joe mcp: connecting to joe at %s\n", serverURL)

	if err := deps.serveMCP(s); err != nil {
		fmt.Fprintf(stderr, "joe mcp: server error: %v\n", err)
		return 1
	}
	return 0
}

// runSlackCommand starts Joe as a Slack bot via Socket Mode.
// Environment variables:
//
//	SLACK_BOT_TOKEN  — Bot User OAuth token (xoxb-...)
//	SLACK_APP_TOKEN  — App-Level token with connections:write scope (xapp-...)
//	JOE_SERVER       — joe server base URL (default: http://localhost:7777)
//	JOE_API_KEY      — Bearer token for joe API auth (optional)
//
// It takes no flags and no positional arguments — configuration comes solely
// from the environment (D-0132 withholds --config here deliberately, since
// this command reads no config file) — so the flag set below exists only to
// catch and reject anything the operator passes, rather than silently
// ignoring it.
func runSlackCommand(ctx context.Context, args []string, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe slack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "Error: slack takes no positional arguments")
		return 2
	}

	botToken := deps.getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		fmt.Fprintln(stderr, "joe slack: SLACK_BOT_TOKEN is required")
		return 1
	}
	appToken := deps.getenv("SLACK_APP_TOKEN")
	if appToken == "" {
		fmt.Fprintln(stderr, "joe slack: SLACK_APP_TOKEN is required (xapp-...)")
		return 1
	}

	serverURL := deps.getenv("JOE_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:7777"
	}
	apiKey := deps.getenv("JOE_API_KEY")

	var clientOpts []client.ClientOption
	if apiKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(apiKey))
	}
	coreClient := deps.newClient(serverURL, clientOpts...)

	api := gslack.New(botToken, gslack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)
	agent := jslack.NewAgent(coreClient)
	srv := jslack.NewServer(api, sm, agent)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("joe slack: starting", "server", serverURL)
	fmt.Fprintf(stderr, "joe slack: connecting to joe at %s\n", serverURL)

	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(stderr, "joe slack: %v\n", err)
		return 1
	}
	return 0
}

// runSkillsCommand implements `joe skills <subcommand>` — install, list,
// remove, update, and (Phase 3) reload Agent Skills components installed at
// ~/.joe/skills/. install/list/remove/update operate on the local filesystem
// only; reload calls into the joe server to refresh its in-memory registry without
// a restart.
func runSkillsCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe skills <install|list|remove|update|approve|reject|reload> [flags]")
		fmt.Fprintln(stderr, "  install <repo-url> [--ref <branch|tag>] [--subdir <path>]")
		fmt.Fprintln(stderr, "                              Clone a skills repo into ~/.joe/skills/. Lands in")
		fmt.Fprintln(stderr, "                              quarantine unless skills-policy.yaml auto-approves it.")
		fmt.Fprintln(stderr, "  list                        Show installed skills with status (active or quarantined).")
		fmt.Fprintln(stderr, "  remove <skill-name> [--force]")
		fmt.Fprintln(stderr, "                              Uninstall the skill. --force is required if its install")
		fmt.Fprintln(stderr, "                              contains other skills.")
		fmt.Fprintln(stderr, "  update [<skill-name>]       Fetch and reset every install, or just the one")
		fmt.Fprintln(stderr, "                              containing the named skill.")
		fmt.Fprintln(stderr, "  approve <skill-name>        Move a quarantined skill into the active registry.")
		fmt.Fprintln(stderr, "  reject  <skill-name>        Delete a quarantined skill from disk.")
		fmt.Fprintln(stderr, "  reload                      Trigger the joe server to rescan ~/.joe/skills/ without a restart.")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Any of the above also accepts --config <path>, naming the same config file the")
		fmt.Fprintln(stderr, "daemon is started with. It governs skills.trusted_sources (which repos install")
		fmt.Fprintln(stderr, "accepts) and the server address reload contacts. The skills directory itself is")
		fmt.Fprintln(stderr, "not configurable and is unaffected.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	// --config is lifted out before dispatch because the config is loaded here,
	// ahead of the sub-subcommand's own flag set. See splitConfigFlag.
	configFlag, args, err := splitConfigFlag(args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	joeDir, err := deps.joeDirPath()
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot determine Joe config directory: %v\n", err)
		return 1
	}

	// Load config so install can enforce trusted_sources and reload can
	// find the joe server's address. A missing config file at the DEFAULT path
	// falls back to defaults — both fields are simply empty in that case, which
	// is the correct behaviour for a fresh install. A missing file at a path the
	// operator named is a failure; see resolveConfigFlag.
	//
	// One load serves both uses, so --config cannot redirect the trusted-source
	// allowlist and the reload target independently.
	cfgPath, ok := resolveConfigFlag(configFlag, stderr)
	if !ok {
		return 1
	}
	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	// Load skills policy from ~/.joe/skills-policy.yaml. A missing file is
	// fine — it falls back to DefaultPolicy() (deny-by-default), which is
	// the safe behavior. A malformed file is fatal: continuing with a
	// silently-dropped policy would flip the system into "trust everything"
	// mode, which is the exact opposite of safe.
	skillsPolicy, err := deps.loadSkillsPolicy(joeDir)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load skills policy: %v\n", err)
		return 1
	}
	mgr := deps.newSkillManager(filepath.Join(joeDir, "skills"), cfg.Skills.TrustedSources, skillsPolicy)

	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("joe skills install", flag.ContinueOnError)
		fs.SetOutput(stderr)
		ref := fs.String("ref", "", "branch, tag, or commit to pin (default: repo's default branch)")
		subdir := fs.String("subdir", "", "install a single skill subdirectory via sparse checkout")
		if err := fs.Parse(reorderFlagsFirst(args[1:], map[string]bool{"ref": true, "subdir": true})); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: install requires exactly one <repo-url> positional argument")
			return 2
		}
		repo := fs.Arg(0)
		install, err := mgr.Install(ctx, repo, *ref, *subdir)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		statusWord := "Installed"
		if install.IsQuarantined() {
			statusWord = "Quarantined"
		}
		fmt.Fprintf(stdout, "%s %s @ %s (%d skill(s)):\n", statusWord, install.Repo, shortCommit(install.Commit), len(install.Skills))
		for _, s := range install.Skills {
			fmt.Fprintf(stdout, "  - %s\n", s.Name)
		}
		if install.IsQuarantined() {
			fmt.Fprintf(stdout, "Reason: %s\n", install.QuarantineReason)
			fmt.Fprintln(stdout, "Run `joe skills approve <name>` to activate, or `joe skills reject <name>` to discard.")
		} else {
			fmt.Fprintln(stdout, "joe will pick up the new skills automatically; run `joe skills reload` if hot reload is disabled.")
		}
		return 0

	case "list":
		// list takes neither flags nor positionals. The flag set exists solely to
		// reject them: without it the sub-subcommand read args[0] to dispatch and
		// never looked at args[1:], so `joe skills list --quarantined` listed
		// everything and reported success, having ignored the flag.
		fs := flag.NewFlagSet("joe skills list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "Error: list takes no positional arguments")
			return 2
		}

		installs, err := mgr.List()
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(installs) == 0 {
			fmt.Fprintln(stdout, "No skills installed. Run `joe skills install <repo-url>` to add one.")
			return 0
		}
		fmt.Fprintf(stdout, "%-32s  %-12s  %-12s  %s\n", "SKILL", "STATUS", "REF", "REPO")
		for _, in := range installs {
			ref := in.Ref
			if ref == "" {
				ref = "(default)"
			}
			status := in.Status
			if status == "" {
				status = skills.InstallStatusActive
			}
			for _, s := range in.Skills {
				fmt.Fprintf(stdout, "%-32s  %-12s  %-12s  %s\n", s.Name, status, ref, in.Repo)
			}
		}
		return 0

	case "remove":
		fs := flag.NewFlagSet("joe skills remove", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := fs.Bool("force", false, "remove the install even if it provides other skills")
		if err := fs.Parse(reorderFlagsFirst(args[1:], map[string]bool{})); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: remove requires exactly one <skill-name>")
			return 2
		}
		removed, err := mgr.Remove(ctx, fs.Arg(0), *force)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(removed) == 1 {
			fmt.Fprintf(stdout, "Removed skill %q.\n", removed[0])
		} else {
			fmt.Fprintf(stdout, "Removed %d skill(s): %s\n", len(removed), strings.Join(removed, ", "))
		}
		fmt.Fprintln(stdout, "joe will drop the removed skills automatically; run `joe skills reload` if hot reload is disabled.")
		return 0

	case "update":
		fs := flag.NewFlagSet("joe skills update", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		target := ""
		if fs.NArg() == 1 {
			target = fs.Arg(0)
		} else if fs.NArg() > 1 {
			fmt.Fprintln(stderr, "Error: update takes at most one <skill-name>")
			return 2
		}
		updated, err := mgr.Update(ctx, target)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(updated) == 0 {
			fmt.Fprintln(stdout, "Nothing to update.")
			return 0
		}
		for _, in := range updated {
			suffix := ""
			if in.IsQuarantined() {
				suffix = " [quarantined: " + in.QuarantineReason + "]"
			}
			fmt.Fprintf(stdout, "Updated %s @ %s (%d skill(s))%s\n", in.Repo, shortCommit(in.Commit), len(in.Skills), suffix)
		}
		fmt.Fprintln(stdout, "Run `joe skills reload` to refresh the joe server without a restart.")
		return 0

	case "approve":
		fs := flag.NewFlagSet("joe skills approve", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: approve requires exactly one <skill-name>")
			return 2
		}
		install, err := mgr.Approve(ctx, fs.Arg(0))
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Approved %s (%d skill(s) now active):\n", install.Repo, len(install.Skills))
		for _, s := range install.Skills {
			fmt.Fprintf(stdout, "  - %s\n", s.Name)
		}
		fmt.Fprintln(stdout, "joe will pick up the approved skills automatically; run `joe skills reload` if hot reload is disabled.")
		return 0

	case "reject":
		fs := flag.NewFlagSet("joe skills reject", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: reject requires exactly one <skill-name>")
			return 2
		}
		removed, err := mgr.Reject(ctx, fs.Arg(0))
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(removed) == 1 {
			fmt.Fprintf(stdout, "Rejected skill %q.\n", removed[0])
		} else {
			fmt.Fprintf(stdout, "Rejected %d skill(s): %s\n", len(removed), strings.Join(removed, ", "))
		}
		return 0

	case "reload":
		fs := flag.NewFlagSet("joe skills reload", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "Error: reload takes no positional arguments")
			return 2
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

		result, err := c.ReloadSkills(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Reload %s: %d skill(s) before, %d after.\n", result.Status, result.Before, result.After)
		if len(result.Added) > 0 {
			fmt.Fprintf(stdout, "  + Added:   %s\n", strings.Join(result.Added, ", "))
		}
		if len(result.Removed) > 0 {
			fmt.Fprintf(stdout, "  - Removed: %s\n", strings.Join(result.Removed, ", "))
		}
		if len(result.Updated) > 0 {
			fmt.Fprintf(stdout, "  ~ Updated: %s\n", strings.Join(result.Updated, ", "))
		}
		if result.Status != "ok" {
			return 1
		}
		return 0

	default:
		fmt.Fprintf(stderr, "Unknown skills subcommand: %s\n\n", args[0])
		usage()
		return 2
	}
}

func shortCommit(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// resolveConfigFlag turns a subcommand's --config value into the config path to
// load, or explains an operational failure and reports ok=false.
//
// Every subcommand carrying --config resolves through this one function, so the
// flag means the same thing everywhere: an operator who learns it on one command
// can reuse the same invocation token verbatim on the next. Extracted from
// `joe admin bootstrap`, which established the rule (D-0131); it is NOT the
// broader config-resolution consolidation docs/backlog/admin-bootstrap-cli-04.md
// describes, which stays undone.
//
// The asymmetry is the point. A missing file at the DEFAULT path is not an
// error — config.Load falls back to defaults, which is correct for a fresh
// install with no config file at all, and each command then fails (or succeeds)
// on its own terms. A missing file at an EXPLICITLY NAMED path is an operational
// failure: the operator asserted that file exists, and silently falling back to
// defaults would point the command at a different install than the one they
// named, which for `joe db backup` means backing up the wrong database and being
// told it succeeded.
//
// The returned path is the operator's value verbatim rather than the expanded
// one — config.Load performs its own "~" expansion, and handing it an
// already-expanded path would make the path it logs differ from the path the
// operator typed.
func resolveConfigFlag(flagValue string, stderr io.Writer) (string, bool) {
	if flagValue == "" {
		return paths.DefaultConfigPath(), true
	}
	resolved, err := paths.ExpandPath(flagValue)
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot resolve --config %s: %v\n", flagValue, err)
		return "", false
	}
	if _, err := os.Stat(resolved); err != nil {
		fmt.Fprintf(stderr, "Error: cannot read the config file named by --config: %v\n", err)
		fmt.Fprintln(stderr, "Name the same config file the daemon is started with, or omit --config to use")
		fmt.Fprintln(stderr, "the default ~/.joe/config.yaml.")
		return "", false
	}
	return flagValue, true
}

// splitConfigFlag lifts a --config token (and its value) out of args, returning
// the value and the remaining arguments.
//
// It exists for the two commands whose first positional selects a
// sub-subcommand — `joe incident` and `joe skills`. Both load their config
// BEFORE dispatching, so --config belongs to neither the outer word nor any one
// sub-flagset, and the stdlib flag package has no notion of a flag that spans
// both. Lifting the token first lets the flag appear anywhere in the invocation,
// which is what keeps one operator invocation reusable verbatim across commands
// that structure their arguments differently.
//
// A trailing --config with no value is a malformed invocation and is reported as
// one, matching what the flag package would have said.
func splitConfigFlag(args []string) (string, []string, error) {
	value := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 >= len(args) {
				return "", nil, errors.New("flag needs an argument: -config")
			}
			value = args[i+1]
			i++
		case strings.HasPrefix(a, "-config=") || strings.HasPrefix(a, "--config="):
			value = a[strings.Index(a, "=")+1:]
		default:
			rest = append(rest, a)
		}
	}
	return value, rest, nil
}

// reorderFlagsFirst moves --flag tokens (and their values for the named
// `valueFlags`) to the front of args so callers can write either
// `cmd <pos> --flag v` or `cmd --flag v <pos>`. The stdlib flag package only
// supports the latter natively.
func reorderFlagsFirst(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		// `--flag=value` carries the value inline; otherwise we need to
		// pull the next token as the value when the flag expects one.
		if strings.Contains(a, "=") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		if valueFlags[name] && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

func runWithDeps(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	// A non-flag first argument selects a subcommand. A leading flag (e.g.
	// `--config`) or no argument at all belongs to the default server path.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "panic":
			return runPanicCommand(ctx, args[1:], stdout, stderr, deps)
		case "unlock":
			return runUnlockCommand(ctx, args[1:], stdout, stderr, deps)
		case "mcp":
			return runMCPCommand(ctx, args[1:], stderr, deps)
		case "slack":
			return runSlackCommand(ctx, args[1:], stderr, deps)
		case "skills":
			return runSkillsCommand(ctx, args[1:], stdout, stderr, deps)
		case "incident":
			return runIncidentCommand(ctx, args[1:], stdout, stderr, deps)
		case "db":
			return runDBCommand(ctx, args[1:], stdout, stderr, deps)
		case "admin":
			return runAdminCommand(ctx, args[1:], stdout, stderr, deps)
		default:
			fmt.Fprintf(stderr, "Unknown command: %q\n\n", args[0])
			printUsage(stderr)
			return 2
		}
	}

	// No subcommand (bare `joe`) or server flags only (e.g. `joe --config ...`):
	// run the HTTP API daemon, which is Joe's default behavior. Its subcommands
	// (mcp, slack, panic, unlock, skills, incident, db, admin) ride alongside.
	// RBAC zone provisioning is not a CLI surface — it runs over the admin REST
	// API (internal/api/admin.go). Admin grants run there too, with the single
	// exception of `joe admin bootstrap`, which grants the FIRST admin on an
	// empty roster and is refused thereafter; see cmd/joe/admin.go.
	return deps.runServer(ctx)
}

// printUsage writes the top-level command summary to w.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: joe [command] [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Run with no command (or only --config) to start the joe server daemon.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  mcp      Run Joe as an MCP stdio server")
	fmt.Fprintln(w, "  slack    Run Joe as a Slack bot")
	fmt.Fprintln(w, "  skills   Manage Agent Skills components")
	fmt.Fprintln(w, "  incident Declare, resolve, or inspect the incident regime")
	fmt.Fprintln(w, "  db       Operate on Joe's database file (backup, restore)")
	fmt.Fprintln(w, "  admin    Bootstrap the first admin on a database that has none")
	fmt.Fprintln(w, "  panic    Trigger an emergency shutdown of the joe server")
	fmt.Fprintln(w, "  unlock   Clear the panic state in the database (idempotent; takes effect on restart)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Every command whose behavior a config file governs accepts --config <path>, with")
	fmt.Fprintln(w, "the same meaning it has on the daemon: name the config file joe was started with,")
	fmt.Fprintln(w, "so the command acts on that install's database and server rather than the default")
	fmt.Fprintln(w, "~/.joe/config.yaml. mcp and slack take their configuration from the environment")
	fmt.Fprintln(w, "(JOE_SERVER, JOE_API_KEY, SLACK_*) and read no config file, so they do not take it.")
}

func main() {
	ctx := context.Background()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
