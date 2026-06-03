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

	mcpserver "github.com/mark3labs/mcp-go/server"
	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/mcp"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/review"
	"github.com/jaimegago/joe/internal/skills"
	jslack "github.com/jaimegago/joe/internal/slack"
)

type runDeps struct {
	loadConfig       func(path string) (*config.Config, error)
	joeDirPath       func() (string, error)
	newClient        func(baseURL string, opts ...client.ClientOption) *client.Client
	newSkillManager  func(root string, trusted []string, policy *skills.Policy) skillManager
	loadSkillsPolicy func(joeDir string) (*skills.Policy, error)
	// openRBACRepo opens the RBAC repository for `joe zone` and
	// `joe admin` provisioning (direct DB access; design §2.9, Phase H
	// D-0011). Injectable so tests use an in-memory repo instead of
	// opening a real database. Returns the full rbac.Repository surface
	// because admin status (Phase H) and zone grants (Phase C) read and
	// write different methods on the same underlying repo.
	openRBACRepo func(cfg *config.Config) (rbacRepo, func() error, error)
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
		openRBACRepo: openRBACRepoDefault,
	}
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
	if key := cfg.Server.LoopbackKey(); key != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(key))
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
	if key := cfg.Server.LoopbackKey(); key != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(key))
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
	if key := cfg.Server.LoopbackKey(); key != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(key))
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
			return 1
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
			fmt.Fprintln(stdout, "joe-core will pick up the new skills automatically; run `joe skills reload` if hot reload is disabled.")
		}
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
			suffix := ""
			if in.IsQuarantined() {
				suffix = " [quarantined: " + in.QuarantineReason + "]"
			}
			fmt.Fprintf(stdout, "Updated %s @ %s (%d skill(s))%s\n", in.Repo, shortCommit(in.Commit), len(in.Skills), suffix)
		}
		fmt.Fprintln(stdout, "Run `joe skills reload` to refresh joe-core without a restart.")
		return 0

	case "approve":
		fs := flag.NewFlagSet("joe skills approve", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: approve requires exactly one <skill-name>")
			return 1
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
		fmt.Fprintln(stdout, "joe-core will pick up the approved skills automatically; run `joe skills reload` if hot reload is disabled.")
		return 0

	case "reject":
		fs := flag.NewFlagSet("joe skills reject", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Error: reject requires exactly one <skill-name>")
			return 1
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
		case "zone":
			return runZoneCommand(ctx, args[1:], stdout, stderr, deps)
		case "admin":
			return runAdminCommand(ctx, args[1:], stdout, stderr, deps)
		}
	}

	// The interactive REPL has been removed: joe is now a launcher for its
	// subcommands (mcp, slack, panic, unlock, review, skills, zone, admin).
	// A bare invocation or unknown subcommand prints usage and exits non-zero.
	printUsage(stderr)
	return 2
}

// printUsage writes the top-level command summary to w.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: joe <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  mcp      Run Joe as an MCP stdio server")
	fmt.Fprintln(w, "  slack    Run Joe as a Slack bot")
	fmt.Fprintln(w, "  review   Manage code-review jobs")
	fmt.Fprintln(w, "  skills   Manage Agent Skills sources")
	fmt.Fprintln(w, "  zone     Manage RBAC zone grants")
	fmt.Fprintln(w, "  admin    Manage admin provisioning")
	fmt.Fprintln(w, "  panic    Trigger an emergency shutdown of joe-core")
	fmt.Fprintln(w, "  unlock   Lift joe-core safe mode")
}

func main() {
	ctx := context.Background()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
