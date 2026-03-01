package slack

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	gslack "github.com/slack-go/slack"
)

// Formatter builds Slack Block Kit messages.
type Formatter struct{}

// NewFormatter creates a Formatter.
func NewFormatter() *Formatter { return &Formatter{} }

// StatusBlocks builds Block Kit blocks for a graph summary.
func (f *Formatter) StatusBlocks(summary *graph.GraphSummary) []gslack.Block {
	blocks := []gslack.Block{
		gslack.NewHeaderBlock(
			gslack.NewTextBlockObject(gslack.PlainTextType, ":bar_chart: Joe Infrastructure Status", true, false),
		),
		gslack.NewDividerBlock(),
		gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType,
				fmt.Sprintf("*Nodes:* %d   *Edges:* %d", summary.NodeCount, summary.EdgeCount),
				false, false,
			),
			nil, nil,
		),
	}

	if len(summary.NodesByType) > 0 {
		var sb strings.Builder
		sb.WriteString("*Nodes by type:*\n")

		types := make([]string, 0, len(summary.NodesByType))
		for t := range summary.NodesByType {
			types = append(types, t)
		}
		sort.Strings(types)

		for _, t := range types {
			sb.WriteString(fmt.Sprintf("• `%s`: %d\n", t, summary.NodesByType[t]))
		}

		blocks = append(blocks, gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType, sb.String(), false, false),
			nil, nil,
		))
	}

	if len(summary.RecentlyAdded) > 0 {
		limit := len(summary.RecentlyAdded)
		if limit > 3 {
			limit = 3
		}
		var sb strings.Builder
		sb.WriteString("*Recently added:*\n")
		for _, n := range summary.RecentlyAdded[:limit] {
			sb.WriteString(fmt.Sprintf("• `%s` (%s)\n", n.ID, n.Type))
		}
		blocks = append(blocks, gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType, sb.String(), false, false),
			nil, nil,
		))
	}

	blocks = append(blocks, gslack.NewContextBlock("",
		gslack.NewTextBlockObject(gslack.MarkdownType,
			fmt.Sprintf("_Last checked: %s_", time.Now().UTC().Format("2006-01-02 15:04 UTC")),
			false, false,
		),
	))

	return blocks
}

// AskBlocks builds Block Kit blocks for a freeform query response.
func (f *Formatter) AskBlocks(query, answer string) []gslack.Block {
	return []gslack.Block{
		gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType,
				fmt.Sprintf(":mag: *Query:* %s", query),
				false, false,
			),
			nil, nil,
		),
		gslack.NewDividerBlock(),
		gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType, answer, false, false),
			nil, nil,
		),
	}
}

// ErrorBlock builds a simple error message block.
func (f *Formatter) ErrorBlock(msg string) []gslack.Block {
	return []gslack.Block{
		gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType,
				fmt.Sprintf(":warning: %s", msg),
				false, false,
			),
			nil, nil,
		),
	}
}

// HelpBlocks returns the help message blocks.
func (f *Formatter) HelpBlocks() []gslack.Block {
	return []gslack.Block{
		gslack.NewHeaderBlock(
			gslack.NewTextBlockObject(gslack.PlainTextType, ":robot_face: Joe Commands", true, false),
		),
		gslack.NewSectionBlock(
			gslack.NewTextBlockObject(gslack.MarkdownType, strings.Join([]string{
				"*`/joe ask <query>`* — search the infrastructure graph and knowledge store",
				"*`/joe status`* — show graph summary (node/edge counts by type)",
				"*`/joe incidents`* — list active incidents (requires Alertmanager source)",
				"*`/joe help`* — show this message",
			}, "\n"), false, false),
			nil, nil,
		),
	}
}
