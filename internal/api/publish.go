package api

import (
	"context"
	"encoding/json"
	"fmt"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	confluencesync "github.com/jaimegago/joe/internal/knowledge/sync/confluence"
	notionsync "github.com/jaimegago/joe/internal/knowledge/sync/notion"
)

// publishProposalToTarget dispatches a proposal to the appropriate write
// adapter. It is a package-level helper driving the in-process loop client
// (inProcessCoreClient.PublishProposal) — the live publish path, which does
// not go through the loopback after Phase E.
func publishProposalToTarget(ctx context.Context, services *core.Services, p *proposals.Proposal) error {
	switch p.TargetType {
	case proposals.TargetConfluence:
		return publishToConfluence(ctx, services, p)
	case proposals.TargetNotion:
		return publishToNotion(ctx, services, p)
	case proposals.TargetGit:
		return publishToGit(ctx, services, p)
	default:
		return fmt.Errorf("unsupported target type: %s", p.TargetType)
	}
}

func publishToConfluence(ctx context.Context, services *core.Services, p *proposals.Proposal) error {
	svc := services.Knowledge
	if svc == nil {
		return fmt.Errorf("knowledge service not available")
	}
	sources, err := svc.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	for _, src := range sources {
		if src.Type != "confluence" {
			continue
		}
		var cfg confluencesync.Config
		if err := json.Unmarshal(src.Config, &cfg); err != nil {
			continue
		}
		// Get current page version for optimistic locking.
		version, err := confluencesync.GetPageVersion(ctx, &cfg, p.TargetID)
		if err != nil {
			return fmt.Errorf("get confluence page version: %w", err)
		}
		return confluencesync.UpdatePage(ctx, &cfg, p.TargetID, p.Title, p.ProposedContent, version+1)
	}
	return fmt.Errorf("no confluence source configured")
}

func publishToNotion(ctx context.Context, services *core.Services, p *proposals.Proposal) error {
	svc := services.Knowledge
	if svc == nil {
		return fmt.Errorf("knowledge service not available")
	}
	sources, err := svc.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	for _, src := range sources {
		if src.Type != "notion" {
			continue
		}
		var cfg notionsync.Config
		if err := json.Unmarshal(src.Config, &cfg); err != nil {
			continue
		}
		return notionsync.UpdatePage(ctx, &cfg, p.TargetID, p.ProposedContent)
	}
	return fmt.Errorf("no notion source configured")
}

func publishToGit(ctx context.Context, services *core.Services, p *proposals.Proposal) error {
	svc := services.Knowledge
	if svc == nil {
		return fmt.Errorf("knowledge service not available")
	}
	sources, err := svc.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	for _, src := range sources {
		if src.Type != "git" {
			continue
		}
		var cfg gitadapter.Config
		if err := json.Unmarshal(src.Config, &cfg); err != nil {
			continue
		}
		auth := gitadapter.DocAuthConfig{
			AuthType:   cfg.AuthType,
			HTTPToken:  cfg.HTTPToken,
			SSHKeyPath: cfg.SSHKeyPath,
		}
		commitMsg := fmt.Sprintf("docs: update %s via Joe", p.TargetID)
		return gitadapter.CommitAndPush(ctx, cfg.URL, cfg.Branch, p.TargetID, p.ProposedContent, commitMsg, auth)
	}
	return fmt.Errorf("no git source configured")
}
