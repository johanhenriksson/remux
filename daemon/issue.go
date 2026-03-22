package daemon

import (
	"context"
	"time"
)

// Issue represents a Linear issue.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	Priority    *int
	State       string
	BranchName  string
	URL         string
	Labels      []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RunEntry tracks an active agent run for an issue.
type RunEntry struct {
	IssueID    string
	Identifier string
	Issue      Issue
	Attempt    int
	StatusFix  bool
	StartedAt  time.Time
	Cancel     context.CancelFunc
	ResultCh   <-chan RunResult
}

// RetryEntry tracks a pending retry for a failed run.
type RetryEntry struct {
	IssueID    string
	Identifier string
	Attempt    int
	StatusFix  bool
	PrevState  string
	DueAt      time.Time
	Error      error
	Timer      *time.Timer
}

// RunResult is sent by a monitor goroutine when an agent run completes.
type RunResult struct {
	IssueID string
	Success bool
	Err     error
}
