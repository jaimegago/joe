package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
	"github.com/jaimegago/joe/internal/logging"
	"github.com/jaimegago/joe/internal/mcp"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/repl"
	"github.com/jaimegago/joe/internal/review"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/skills"
	jslack "github.com/jaimegago/joe/internal/slack"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/useragent"
)

type replRunner interface {
	Run(ctx context.Context) error
}

type runDeps struct {
	loadConfig      func(path string) (*config.Config, error)
	setupOTel       func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error)
	newMetrics      func() *observability.Metrics
	newAdapter      func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error)
	joeDirPath      func() (string, error)
	loadPolicy      func(configDir string) (*safety.SafetyPolicy, error)
	newClient       func(baseURL string, opts ...client.ClientOption) *client.Client
	newRegistry     func(coreClient *client.Client, policy *safety.SafetyPolicy) *tools.Registry
	newExecutor     func(registry *tools.Registry, metrics *observability.Metrics, opts ...tools.ExecutorOption) *tools.Executor
	newRepl         func(agent *useragent.Agent, cfg *config.Config, session *useragent.Session) replRunner
	newSkillManager func(root string, trusted []string) skillManager
}

func defaultRunDeps() runDeps {
	return runDeps{
		loadConfig:  config.Load,
		setupOTel:   observability.Setup,
		newMetrics:  observability.NewMetrics,
		newAdapter:  llmfactory.NewAdapter,
		joeDirPath:  paths.JoeDirPath,
		loadPolicy:  safety.LoadPolicy,
		newClient:   client.New,
		newRegistry: tools.NewDefaultRegistryWithClient,
		newExecutor: tools.NewExecutor,
		newRepl: func(agent *useragent.Agent, cfg *config.Config, session *useragent.Session) replRunner {
			return repl.NewWithSession(agent, cfg, session)
		},
		newSkillManager: func(root string, trusted []string) skillManager {
			return skills.NewManager(root, nil).WithTrustedSources(trusted)
		},
	}
}

// skillManager is the narrow surface the `joe skills` CLI needs from the
// install manager. It exists so tests can inject a fake without spawning git.
type skillManager interface {
	Install(ctx context.Context, repo, ref, subdir string) (*skills.Install, error)
	Remove(ctx context.Context, name string, force bool) ([]string, error)
	Update(ctx context.Context, name string) ([]*skills.Install, error)
	List() ([]skills.Install, error)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithDeps(ctx, args, stdout, stderr, defaultRunDeps())
}

// runPanicCommand sends an emergency shutdown request to joe-core.
func runPanicCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe panic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	reason := fs.String("reason", "operator triggered via CLI", "reason for the emergency shutdown")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := deps.loadConfig(*configPath)
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
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	if err := c.TriggerPanic(ctx, *reason); err != nil {
		fmt.Fprintf(stderr, "Error: failed to trigger panic: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Emergency shutdown triggered. joe-core will restart in safe mode.")
	fmt.Fprintln(stdout, "Use 'joe unlock --reason \"...\"' to resume normal operation.")
	return 0
}

// runUnlockCommand exits joe-core's safe mode.
func runUnlockCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe unlock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	reason := fs.String("reason", "", "reason for unlocking (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *reason == "" {
		fmt.Fprintln(stderr, "Error: --reason is required")
		fmt.Fprintln(stderr, "Usage: joe unlock --reason \"incident resolved\"")
		return 1
	}

	cfg, err := deps.loadConfig(*configPath)
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
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	if err := c.Unlock(ctx, *reason); err != nil {
		fmt.Fprintf(stderr, "Error: failed to unlock: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Safe mode lifted. Normal operation resumed.")
	return 0
}

// runReviewCommand handles the `joe review` subcommand for managing review jobs.
func runReviewCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: joe review <enqueue|list|get> [flags]")
		fmt.Fprintln(stderr, "  enqueue  --platform <github|gitlab> --source <id> --owner <owner> --repo <repo> --pr <number>")
		fmt.Fprintln(stderr, "  list     [--platform <github|gitlab>] [--status <pending|running|done|failed>] [--limit N]")
		fmt.Fprintln(stderr, "  get      <job-id>")
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
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	switch args[0] {
	case "enqueue":
		fs := flag.NewFlagSet("joe review enqueue", flag.ContinueOnError)
		fs.SetOutput(stderr)
		platform := fs.String("platform", "github", "platform: github or gitlab")
		sourceID := fs.String("source", "", "source ID (required)")
		owner := fs.String("owner", "", "repository owner or GitLab project ID (required)")
		repo := fs.String("repo", "", "repository name (required for GitHub)")
		pr := fs.Int("pr", 0, "PR/MR number (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if *sourceID == "" || *owner == "" || *pr == 0 {
			fmt.Fprintln(stderr, "Error: --source, --owner, and --pr are required")
			return 1
		}
		job := &review.ReviewJob{
			Platform: review.Platform(*platform),
			SourceID: *sourceID,
			Owner:    *owner,
			Repo:     *repo,
			PRNumber: *pr,
		}
		created, err := c.EnqueueReview(ctx, job)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Review job enqueued: %s (status: %s)\n", created.ID, created.Status)
		return 0

	case "list":
		fs := flag.NewFlagSet("joe review list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		platform := fs.String("platform", "", "filter by platform")
		status := fs.String("status", "", "filter by status")
		limit := fs.Int("limit", 20, "maximum number of results")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		jobs, err := c.ListReviews(ctx, review.Platform(*platform), review.JobStatus(*status), *limit)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(jobs) == 0 {
			fmt.Fprintln(stdout, "No review jobs found.")
			return 0
		}
		fmt.Fprintf(stdout, "%-36s  %-8s  %-8s  %s/%s #%s\n", "ID", "PLATFORM", "STATUS", "OWNER", "REPO", "PR")
		for _, j := range jobs {
			fmt.Fprintf(stdout, "%-36s  %-8s  %-8s  %s/%s #%s\n",
				j.ID, j.Platform, j.Status, j.Owner, j.Repo, strconv.Itoa(j.PRNumber))
		}
		return 0

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "Usage: joe review get <job-id>")
			return 2
		}
		job, err := c.GetReview(ctx, args[1])
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ID:       %s\n", job.ID)
		fmt.Fprintf(stdout, "Platform: %s\n", job.Platform)
		fmt.Fprintf(stdout, "Status:   %s\n", job.Status)
		fmt.Fprintf(stdout, "Repo:     %s/%s #%d\n", job.Owner, job.Repo, job.PRNumber)
		if job.Error != "" {
			fmt.Fprintf(stdout, "Error:    %s\n", job.Error)
		}
		if job.ReviewBody != "" {
			fmt.Fprintf(stdout, "\n--- Review ---\n%s\n", job.ReviewBody)
		}
		return 0

	default:
		fmt.Fprintf(stderr, "Unknown review subcommand: %s\n", args[0])
		return 2
	}
}

// runMCPCommand starts Joe as an MCP stdio server.
// Connection details are read from environment variables:
//
//	JOE_SERVER  — joe-core base URL (default: http://localhost:7777)
//	JOE_API_KEY — Bearer token for joe-core API auth (optional)
func runMCPCommand(_ context.Context, _ []string, stderr io.Writer, deps runDeps) int {
	serverURL := os.Getenv("JOE_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:7777"
	}
	apiKey := os.Getenv("JOE_API_KEY")

	var opts []client.ClientOption
	if apiKey != "" {
		opts = append(opts, client.WithAPIKey(apiKey))
	}

	coreClient := deps.newClient(serverURL, opts...)
	s := mcp.NewServer(coreClient)

	fmt.Fprintf(stderr, "joe mcp: connecting to joe-core at %s\n", serverURL)

	if err := mcpserver.ServeStdio(s); err != nil {
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
//	JOE_SERVER       — joe-core base URL (default: http://localhost:7777)
//	JOE_API_KEY      — Bearer token for joe-core API auth (optional)
func runSlackCommand(ctx context.Context, _ []string, stderr io.Writer, deps runDeps) int {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		fmt.Fprintln(stderr, "joe slack: SLACK_BOT_TOKEN is required")
		return 1
	}
	appToken := os.Getenv("SLACK_APP_TOKEN")
	if appToken == "" {
		fmt.Fprintln(stderr, "joe slack: SLACK_APP_TOKEN is required (xapp-...)")
		return 1
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
	coreClient := deps.newClient(serverURL, clientOpts...)

	api := gslack.New(botToken, gslack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)
	agent := jslack.NewAgent(coreClient)
	srv := jslack.NewServer(api, sm, agent)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("joe slack: starting", "server", serverURL)
	fmt.Fprintf(stderr, "joe slack: connecting to joe-core at %s\n", serverURL)

	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(stderr, "joe slack: %v\n", err)
		return 1
	}
	return 0
}

// runSkillsCommand implements `joe skills <subcommand>` — install, list,
// remove, update, and (Phase 3) reload Agent Skills sources installed at
// ~/.joe/skills/. install/list/remove/update operate on the local filesystem
// only; reload calls into joe-core to refresh its in-memory registry without
// a restart.
func runSkillsCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe skills <install|list|remove|update|reload> [flags]")
		fmt.Fprintln(stderr, "  install <repo-url> [--ref <branch|tag>] [--subdir <path>]")
		fmt.Fprintln(stderr, "                              Clone a skills repo into ~/.joe/skills/.")
		fmt.Fprintln(stderr, "  list                        Show installed skills, their source repos, and git refs.")
		fmt.Fprintln(stderr, "  remove <skill-name> [--force]")
		fmt.Fprintln(stderr, "                              Uninstall the skill. --force is required if its install")
		fmt.Fprintln(stderr, "                              contains other skills.")
		fmt.Fprintln(stderr, "  update [<skill-name>]       Fetch and reset every install, or just the one")
		fmt.Fprintln(stderr, "                              containing the named skill.")
		fmt.Fprintln(stderr, "  reload                      Trigger joe-core to rescan ~/.joe/skills/ without a restart.")
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
	// find joe-core's address. A missing config file falls back to
	// defaults — both fields are simply empty in that case, which is
	// the correct behaviour for a fresh install.
	cfg, err := deps.loadConfig(paths.DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}
	mgr := deps.newSkillManager(filepath.Join(joeDir, "skills"), cfg.Skills.TrustedSources)

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
			return 1
		}
		repo := fs.Arg(0)
		install, err := mgr.Install(ctx, repo, *ref, *subdir)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Installed %s @ %s (%d skill(s)):\n", install.Repo, shortCommit(install.Commit), len(install.Skills))
		for _, s := range install.Skills {
			fmt.Fprintf(stdout, "  - %s\n", s.Name)
		}
		fmt.Fprintln(stdout, "joe-core will pick up the new skills automatically; run `joe skills reload` if hot reload is disabled.")
		return 0

	case "list":
		installs, err := mgr.List()
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(installs) == 0 {
			fmt.Fprintln(stdout, "No skills installed. Run `joe skills install <repo-url>` to add one.")
			return 0
		}
		fmt.Fprintf(stdout, "%-32s  %-12s  %s\n", "SKILL", "REF", "REPO")
		for _, in := range installs {
			ref := in.Ref
			if ref == "" {
				ref = "(default)"
			}
			for _, s := range in.Skills {
				fmt.Fprintf(stdout, "%-32s  %-12s  %s\n", s.Name, ref, in.Repo)
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
			return 1
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
		fmt.Fprintln(stdout, "joe-core will drop the removed skills automatically; run `joe skills reload` if hot reload is disabled.")
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
			return 1
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
			fmt.Fprintf(stdout, "Updated %s @ %s (%d skill(s))\n", in.Repo, shortCommit(in.Commit), len(in.Skills))
		}
		fmt.Fprintln(stdout, "Run `joe skills reload` to refresh joe-core without a restart.")
		return 0

	case "reload":
		fs := flag.NewFlagSet("joe skills reload", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "Error: reload takes no positional arguments")
			return 1
		}

		scheme := "http"
		if cfg.Server.TLSEnabled {
			scheme = "https"
		}
		joecoreURL := scheme + "://" + cfg.Server.Address
		var clientOpts []client.ClientOption
		if cfg.Server.APIKey != "" {
			clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
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
	// Dispatch subcommands before parsing REPL flags.
	if len(args) > 0 {
		switch args[0] {
		case "panic":
			return runPanicCommand(ctx, args[1:], stdout, stderr, deps)
		case "unlock":
			return runUnlockCommand(ctx, args[1:], stdout, stderr, deps)
		case "review":
			return runReviewCommand(ctx, args[1:], stdout, stderr, deps)
		case "mcp":
			return runMCPCommand(ctx, args[1:], stderr, deps)
		case "slack":
			return runSlackCommand(ctx, args[1:], stderr, deps)
		case "skills":
			return runSkillsCommand(ctx, args[1:], stdout, stderr, deps)
		}
	}

	fs := flag.NewFlagSet("joe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Initialize a basic logger before config is available
	initialLogger := logging.SetupLogger(logging.LevelInfo)
	slog.SetDefault(initialLogger)

	// Load configuration
	cfg, err := deps.loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	// Initialize OpenTelemetry (default to no tracing in CLI unless explicitly enabled)
	otelCfg := observability.DefaultConfig()
	if _, ok := os.LookupEnv("OTEL_TRACES_ENABLED"); !ok {
		otelCfg.TracesEnabled = false
	}
	if _, ok := os.LookupEnv("OTEL_TRACES_EXPORTER"); !ok {
		otelCfg.TracesExporter = "none"
	}
	shutdownOTel, err := deps.setupOTel(ctx, otelCfg)
	if err != nil {
		slog.Warn("OpenTelemetry setup failed", "error", err)
	} else {
		defer func() { _ = shutdownOTel(context.Background()) }()
	}

	// Create metrics instance
	metrics := deps.newMetrics()

	// Validate LLM configuration and check API keys
	currentModel, err := cfg.LLM.CurrentModel()
	if err != nil {
		fmt.Fprintf(stderr, "You need to connect Joe to an LLM.\n\n%v\n\nCheck your config file's llm.current and llm.available sections.\n", err)
		return 1
	}
	if err := config.ValidateAPIKeysWithUserMessage(currentModel); err != nil {
		fmt.Fprintln(stderr, err.Error())
		fmt.Fprintln(stderr)
		return 1
	}

	// Connect to joe-core
	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	joecoreURL := scheme + "://" + cfg.Server.Address
	var clientOpts []client.ClientOption
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	if cfg.Server.TLSEnabled {
		clientOpts = append(clientOpts, client.WithTLS())
	}
	coreClient := deps.newClient(joecoreURL, clientOpts...)

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := coreClient.Ping(pingCtx); err != nil {
		fmt.Fprintf(stderr, "Error: Cannot connect to joe-core at %s\n", joecoreURL)
		fmt.Fprintf(stderr, "Make sure joe-core is running: joe-core\n\n")
		return 1
	}

	// Set up structured logging based on config
	logger, logCleanup := logging.SetupLoggerWithFile(cfg.Logging.Level, cfg.Logging.File)
	defer logCleanup()
	slog.SetDefault(logger)

	// Log debug mode if enabled
	if cfg.Logging.Level == logging.LevelDebug {
		slog.Debug("running in debug mode")
		fmt.Fprintln(stdout, "Debug mode enabled")
	}

	// Initialize LLM adapter using factory
	baseAdapter, err := deps.newAdapter(ctx, currentModel)
	if err != nil {
		slog.Error("failed to create LLM adapter", "error", err)
		return 1
	}

	// Clean up adapter resources (important for Gemini client)
	if closer, ok := baseAdapter.(io.Closer); ok {
		defer closer.Close()
	}

	// Wrap with instrumentation
	llmAdapter := llm.NewInstrumentedAdapter(baseAdapter, logger, currentModel.Provider, currentModel.Model)

	// Log which model we're using
	slog.Info("LLM initialized",
		"provider", currentModel.Provider,
		"model", currentModel.Model,
	)
	fmt.Fprintf(stdout, "Using %s/%s\n", currentModel.Provider, currentModel.Model)

	// Load safety policy from ~/.joe/safety-policy.yaml
	// If the file doesn't exist, DefaultPolicy is used (most restrictive for T3).
	// If the file is malformed, we refuse to start.
	joeDir, err := deps.joeDirPath()
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot determine Joe config directory: %v\n", err)
		return 1
	}
	safetyPolicy, err := deps.loadPolicy(joeDir)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	slog.Info("safety policy loaded", "config_dir", joeDir)

	// Create tool registry with local tools + core tools (graph_query, graph_related)
	// Pass safety policy so tool-specific settings (e.g., allowed_directories) are enforced.
	registry := deps.newRegistry(coreClient, safetyPolicy)

	// Create REPL notifier for T3 pre-execution countdown and T2/T3 post-execution log
	replNotifier := repl.NewNotifier()

	// Create tool executor with safety policy enforcement and notifications
	executor := deps.newExecutor(registry, metrics,
		tools.WithPolicy(safetyPolicy),
		tools.WithNotifier(replNotifier),
	)

	// Create adapter factory for hot-swapping models
	adapterFactory := func(ctx context.Context, provider, model string) (llm.LLMAdapter, error) {
		// Find the model config
		var modelCfg config.ModelConfig
		found := false
		for _, mc := range cfg.LLM.Available {
			if mc.Provider == provider && mc.Model == model {
				modelCfg = mc
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("model config not found for provider=%s model=%s", provider, model)
		}

		// Validate API keys before creating adapter
		if err := config.ValidateAPIKeys(modelCfg); err != nil {
			return nil, fmt.Errorf("cannot switch to %s: %w", provider, err)
		}

		// Create the base adapter
		baseAdptr, err := llmfactory.NewAdapter(ctx, modelCfg)
		if err != nil {
			return nil, err
		}

		// Wrap with instrumentation
		return llm.NewInstrumentedAdapter(baseAdptr, logger, provider, model), nil
	}

	// Create agent with system prompt and adapter factory
	systemPrompt := `You are Joe, an infrastructure assistant. You can use tools to help answer questions. Be concise.

When you need to access infrastructure resources (Kubernetes, Git, etc.), you'll need source IDs:
- If you don't know the available sources, call list_sources first to discover them
- Then use the source_id from list_sources in subsequent tool calls like k8s_get or k8s_logs
- If there's only one source of the needed type, use that one automatically

You have access to a knowledge store via the search_knowledge tool. Use it proactively when:
- Asked about how something works, known issues, or operational patterns
- Troubleshooting — relevant runbooks or failure modes may already be documented
- Before answering from general knowledge, check if curated or synced docs are available`
	agentInstance := useragent.NewAgent(
		llmAdapter,
		executor,
		registry,
		systemPrompt,
		useragent.WithAdapterFactory(adapterFactory),
		useragent.WithCurrentModelName(cfg.LLM.Current),
	)

	// Create session with message history limit to prevent unbounded growth
	session := useragent.NewSession(metrics)
	session.MaxMessages = useragent.DefaultMaxMessages

	// Create and run REPL (pass config for model management and the session)
	replInstance := deps.newRepl(agentInstance, cfg, session)
	if err := replInstance.Run(ctx); err != nil {
		slog.Error("repl failed", "error", err)
		return 1
	}

	return 0
}

func main() {
	ctx := context.Background()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
