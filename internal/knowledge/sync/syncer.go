// Package sync provides background synchronisation of external knowledge sources
// (Confluence spaces, Notion databases) into the Joe knowledge store.
package sync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/knowledge"
)

// Syncer fetches documents from an external source and upserts them as Tier 2
// knowledge entries.
type Syncer interface {
	// Sync fetches all documents from the source and upserts them via svc.
	Sync(ctx context.Context, src *knowledge.KnowledgeSource, svc *knowledge.Service) error
}

// Coordinator polls the knowledge_sources table and dispatches syncs for each
// active source at its configured interval.
type Coordinator struct {
	svc     *knowledge.Service
	syncers map[string]Syncer // keyed by KnowledgeSource.Type
	logger  *slog.Logger
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewCoordinator creates a new sync Coordinator.
// syncers maps source type strings (e.g. "confluence", "notion") to their
// Syncer implementations.
func NewCoordinator(svc *knowledge.Service, syncers map[string]Syncer) *Coordinator {
	return &Coordinator{
		svc:     svc,
		syncers: syncers,
		logger:  slog.Default(),
		stopCh:  make(chan struct{}),
	}
}

// Start launches the background sync loop. It returns immediately.
func (c *Coordinator) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.loop(ctx)
}

// Stop signals the coordinator to stop and waits for the loop to exit.
func (c *Coordinator) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Coordinator) loop(ctx context.Context) {
	defer c.wg.Done()

	// Poll every minute; individual sources have their own interval gate.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// Run once immediately on start.
	c.runAll(ctx)

	for {
		select {
		case <-ticker.C:
			c.runAll(ctx)
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *Coordinator) runAll(ctx context.Context) {
	sources, err := c.svc.ListSources(ctx)
	if err != nil {
		c.logger.Warn("knowledge sync: failed to list sources", "error", err)
		return
	}
	for _, src := range sources {
		if src.Status != "active" {
			continue
		}
		if !c.isDue(src) {
			continue
		}
		s, ok := c.syncers[src.Type]
		if !ok {
			c.logger.Debug("knowledge sync: no syncer for source type", "type", src.Type)
			continue
		}
		c.syncSource(ctx, src, s)
	}
}

// isDue reports whether the source is due for a sync based on its interval.
func (c *Coordinator) isDue(src *knowledge.KnowledgeSource) bool {
	if src.LastSyncAt == nil {
		return true
	}
	interval := time.Duration(src.SyncIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 60 * time.Minute
	}
	return time.Since(*src.LastSyncAt) >= interval
}

func (c *Coordinator) syncSource(ctx context.Context, src *knowledge.KnowledgeSource, s Syncer) {
	c.logger.Info("knowledge sync: starting", "source_id", src.ID, "type", src.Type)
	start := time.Now()

	err := s.Sync(ctx, src, c.svc)
	lastErr := ""
	if err != nil {
		lastErr = err.Error()
		c.logger.Warn("knowledge sync: failed", "source_id", src.ID, "error", err)
	} else {
		c.logger.Info("knowledge sync: completed", "source_id", src.ID, "duration", time.Since(start))
	}

	if updateErr := c.svc.UpdateSourceSyncStatus(ctx, src.ID, time.Now().UTC(), lastErr); updateErr != nil {
		c.logger.Warn("knowledge sync: failed to update sync status", "source_id", src.ID, "error", updateErr)
	}
}
