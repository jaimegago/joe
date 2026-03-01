package review

import (
	"fmt"
	"time"
)

// JobStatus represents the lifecycle state of a review job.
type JobStatus string

const (
	JobStatusPending JobStatus = "pending"
	JobStatusRunning JobStatus = "running"
	JobStatusDone    JobStatus = "done"
	JobStatusFailed  JobStatus = "failed"
	JobStatusSkipped JobStatus = "skipped" // duplicate event, returned to caller
)

// Platform identifies the forge hosting the PR/MR.
type Platform string

const (
	PlatformGitHub Platform = "github"
	PlatformGitLab Platform = "gitlab"
)

// ReviewJob represents a queued code review task.
type ReviewJob struct {
	ID string `json:"id"`
	// EventID is the deduplication key: "<platform>:<owner>/<repo>#<prNumber>:<headSHA>"
	EventID    string     `json:"event_id"`
	Platform   Platform   `json:"platform"`
	SourceID   string     `json:"source_id"`
	Owner      string     `json:"owner"`
	Repo       string     `json:"repo"`
	PRNumber   int        `json:"pr_number"`
	HeadSHA    string     `json:"head_sha"`
	Status     JobStatus  `json:"status"`
	ReviewBody string     `json:"review_body,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// BuildEventID constructs the canonical deduplication key for a PR/MR event.
func BuildEventID(platform Platform, owner, repo string, prNumber int, headSHA string) string {
	return fmt.Sprintf("%s:%s/%s#%d:%s", platform, owner, repo, prNumber, headSHA)
}
