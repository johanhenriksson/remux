package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/johanhenriksson/remux/spaces"
)

// Orchestrator polls Linear for issues and dispatches agent runs.
type Orchestrator struct {
	workflowPath string
	repoRoot     string
	destDir      string

	workflow *Workflow
	linear   *LinearClient

	viewerID string

	running  map[string]*RunEntry
	claimed  map[string]bool
	retries  map[string]*RetryEntry
	resultCh chan RunResult
	retryCh  chan string
}

// New creates a new Orchestrator.
func New(workflowPath, repoRoot, destDir string) *Orchestrator {
	return &Orchestrator{
		workflowPath: workflowPath,
		repoRoot:     repoRoot,
		destDir:      destDir,
		running:      make(map[string]*RunEntry),
		claimed:      make(map[string]bool),
		retries:      make(map[string]*RetryEntry),
		resultCh:     make(chan RunResult, 16),
		retryCh:      make(chan string, 16),
	}
}

// Run starts the orchestrator loop. Blocks until ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	wf, err := LoadWorkflow(o.workflowPath)
	if err != nil {
		return fmt.Errorf("load workflow: %w", err)
	}
	o.workflow = wf

	o.linear = NewLinearClient(wf.Config.Tracker.Endpoint, wf.Config.Tracker.APIKey)

	viewerID, err := o.linear.FetchViewerID()
	if err != nil {
		return fmt.Errorf("fetch api user: %w", err)
	}
	o.viewerID = viewerID
	log.Printf("authenticated as user %s", viewerID)

	o.cleanupTerminal()

	o.tick(ctx)

	ticker := time.NewTicker(o.workflow.Config.Polling.Interval)
	defer ticker.Stop()

	log.Printf("daemon started: polling every %s, max %d concurrent agents",
		o.workflow.Config.Polling.Interval, o.workflow.Config.Agent.MaxConcurrent)

	for {
		select {
		case <-ctx.Done():
			log.Println("daemon shutting down")
			return nil

		case <-ticker.C:
			o.tick(ctx)

		case result := <-o.resultCh:
			o.handleResult(ctx, result)

		case issueID := <-o.retryCh:
			o.handleRetry(ctx, issueID)
		}
	}
}

func (o *Orchestrator) issueFilter(states []string) IssueFilter {
	cfg := o.workflow.Config.Tracker
	return IssueFilter{
		ProjectSlug: cfg.ProjectSlug,
		States:      states,
		Labels:      cfg.Labels,
		AssigneeID:  o.viewerID,
		CreatorID:   o.viewerID,
	}
}

func (o *Orchestrator) cleanupTerminal() {
	cfg := o.workflow.Config.Tracker
	issues, err := o.linear.FetchIssues(o.issueFilter(cfg.TerminalStates))
	if err != nil {
		log.Printf("cleanup: fetch terminal issues: %v", err)
		return
	}

	for _, issue := range issues {
		branch := issueBranch(issue)
		repoName := filepath.Base(o.repoRoot)
		spaceName := fmt.Sprintf("%s-%s", repoName, spaces.SlugifyBranch(branch))
		worktreePath := filepath.Join(o.destDir, spaceName)

		if _, statErr := os.Stat(worktreePath); statErr == nil {
			log.Printf("[%s] cleaning up terminal workspace", issue.Identifier)
			CleanupWorkspace(issue, o.repoRoot, o.destDir)
		}
	}
}

func (o *Orchestrator) tick(ctx context.Context) {
	wf, err := LoadWorkflow(o.workflowPath)
	if err != nil {
		log.Printf("reload workflow: %v (keeping last good config)", err)
	} else {
		o.workflow = wf
		o.linear = NewLinearClient(wf.Config.Tracker.Endpoint, wf.Config.Tracker.APIKey)
	}

	o.reconcile(ctx)
	o.cleanupTerminal()

	cfg := o.workflow.Config.Tracker
	candidates, err := o.linear.FetchIssues(o.issueFilter(cfg.ActiveStates()))
	if err != nil {
		log.Printf("fetch candidates: %v", err)
		return
	}

	sort.Slice(candidates, func(i, j int) bool {
		pi, pj := candidates[i].Priority, candidates[j].Priority
		if pi != nil && pj != nil {
			if *pi != *pj {
				return *pi < *pj
			}
		} else if pi != nil {
			return true
		} else if pj != nil {
			return false
		}

		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].Identifier < candidates[j].Identifier
	})

	for _, issue := range candidates {
		if o.claimed[issue.ID] {
			continue
		}
		if len(o.running) >= o.workflow.Config.Agent.MaxConcurrent {
			log.Printf("tick: max concurrent reached (%d), skipping remaining", o.workflow.Config.Agent.MaxConcurrent)
			break
		}
		o.dispatch(ctx, issue, 1)
	}
}

func (o *Orchestrator) reconcile(_ context.Context) {
	if len(o.running) == 0 {
		return
	}

	ids := make([]string, 0, len(o.running))
	for id := range o.running {
		ids = append(ids, id)
	}

	states, err := o.linear.FetchIssueStatesByIDs(ids)
	if err != nil {
		log.Printf("reconcile: fetch states: %v (keeping running)", err)
		return
	}

	cfg := o.workflow.Config.Tracker
	terminalSet := make(map[string]bool, len(cfg.TerminalStates))
	for _, s := range cfg.TerminalStates {
		terminalSet[strings.ToLower(s)] = true
	}

	// build set of valid running states: active states + all progress statuses
	validRunning := make(map[string]bool)
	for _, step := range cfg.Steps {
		validRunning[strings.ToLower(step.TriggerStatus)] = true
		if step.ProgressStatus != "" {
			validRunning[strings.ToLower(step.ProgressStatus)] = true
		}
	}

	for id, entry := range o.running {
		state, found := states[id]
		if !found {
			log.Printf("[%s] issue not found during reconcile, cancelling", entry.Identifier)
			entry.Cancel()
			continue
		}

		lower := strings.ToLower(state)
		if terminalSet[lower] {
			log.Printf("[%s] issue reached terminal state %q, cancelling and cleaning up", entry.Identifier, state)
			entry.Cancel()
			CleanupWorkspace(entry.Issue, o.repoRoot, o.destDir)
			continue
		}

		if validRunning[lower] {
			if entry.Issue.State != state {
				log.Printf("[%s] reconcile: state changed %q -> %q", entry.Identifier, entry.Issue.State, state)
			}
			entry.Issue.State = state
		} else {
			log.Printf("[%s] reconcile: state %q not in valid running set %v, not updating", entry.Identifier, state, validRunning)
		}
	}
}

func priorityName(p *int) string {
	if p == nil {
		return "none"
	}
	switch *p {
	case 0:
		return "none"
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 3:
		return "medium"
	case 4:
		return "low"
	default:
		return fmt.Sprintf("%d", *p)
	}
}

func (o *Orchestrator) dispatch(ctx context.Context, issue Issue, attempt int) {
	branch := issueBranch(issue)
	msBranch := milestoneBranch(issue, o.repoRoot)
	labels := "none"
	if len(issue.Labels) > 0 {
		labels = strings.Join(issue.Labels, ", ")
	}

	stepKey, step, ok := o.workflow.Config.Tracker.StepForState(issue.State)
	if !ok {
		log.Printf("[%s] no workflow step for state %q, skipping", issue.Identifier, issue.State)
		return
	}

	log.Printf("[%s] picking up issue (attempt %d)", issue.Identifier, attempt)
	log.Printf("[%s]   title:    %s", issue.Identifier, issue.Title)
	log.Printf("[%s]   step:     %s", issue.Identifier, stepKey)
	log.Printf("[%s]   state:    %s", issue.Identifier, issue.State)
	log.Printf("[%s]   priority: %s", issue.Identifier, priorityName(issue.Priority))
	log.Printf("[%s]   labels:   %s", issue.Identifier, labels)
	log.Printf("[%s]   branch:   %s", issue.Identifier, branch)
	if msBranch != "" {
		log.Printf("[%s]   milestone: %s (branch: %s)", issue.Identifier, issue.Milestone, msBranch)
	}
	log.Printf("[%s]   url:      %s", issue.Identifier, issue.URL)

	// move to progress status
	if step.ProgressStatus != "" {
		if err := o.linear.UpdateIssueState(issue.ID, step.ProgressStatus); err != nil {
			log.Printf("[%s] update to progress status %q: %v", issue.Identifier, step.ProgressStatus, err)
		} else {
			log.Printf("[%s] moved to %q", issue.Identifier, step.ProgressStatus)
		}
	}

	// create tracking comment
	commentHeader := fmt.Sprintf("**%s**: %s - %s\nMoving from %s → %s",
		stepKey, issue.Identifier, issue.Title, issue.State, step.ProgressStatus)

	var logger *CommentLogger
	commentID, err := o.linear.CreateComment(issue.ID, commentHeader)
	if err != nil {
		log.Printf("[%s] create comment: %v", issue.Identifier, err)
	} else {
		logger = NewCommentLogger(o.linear, commentID, commentHeader)
	}

	// revertStatus reverts the issue back to its trigger status on dispatch failure.
	revertStatus := func() {
		if step.ProgressStatus != "" {
			if err := o.linear.UpdateIssueState(issue.ID, step.TriggerStatus); err != nil {
				log.Printf("[%s] revert to trigger status %q: %v", issue.Identifier, step.TriggerStatus, err)
			} else {
				log.Printf("[%s] reverted to %q", issue.Identifier, step.TriggerStatus)
			}
		}
	}

	workspacePath, err := EnsureWorkspace(issue, o.repoRoot, o.destDir, msBranch)
	if err != nil {
		log.Printf("[%s] ensure workspace: %v", issue.Identifier, err)
		revertStatus()
		return
	}

	if err := EnsureSession(workspacePath, o.destDir); err != nil {
		log.Printf("[%s] ensure session: %v", issue.Identifier, err)
		revertStatus()
		return
	}

	attemptPtr := &attempt
	prompt, err := o.workflow.RenderPrompt(issue, step.Name, attemptPtr, msBranch)
	if err != nil {
		log.Printf("[%s] render prompt: %v", issue.Identifier, err)
		revertStatus()
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	resultCh, err := LaunchAgent(runCtx, issue, o.workflow.Config.Agent.Command, prompt, workspacePath, logger)
	if err != nil {
		cancel()
		log.Printf("[%s] launch agent: %v", issue.Identifier, err)
		revertStatus()
		return
	}

	entry := &RunEntry{
		IssueID:         issue.ID,
		Identifier:      issue.Identifier,
		Issue:           issue,
		Attempt:         attempt,
		StepKey:         stepKey,
		CommentID:       commentID,
		MilestoneBranch: msBranch,
		StartedAt:       time.Now(),
		Cancel:          cancel,
		ResultCh:        resultCh,
	}

	o.running[issue.ID] = entry
	o.claimed[issue.ID] = true

	go func() {
		result := <-resultCh
		o.resultCh <- result
	}()
}

func (o *Orchestrator) handleResult(_ context.Context, result RunResult) {
	entry, ok := o.running[result.IssueID]
	if !ok {
		return
	}
	delete(o.running, result.IssueID)

	if result.Success {
		log.Printf("[%s] agent completed successfully (attempt %d)", entry.Identifier, entry.Attempt)

		log.Printf("[%s] looking up step for state %q (stepKey=%q)", entry.Identifier, entry.Issue.State, entry.StepKey)
		step, ok := o.workflow.Config.Tracker.Steps[entry.StepKey]
		if !ok {
			log.Printf("[%s] stepKey %q not found in current workflow config", entry.Identifier, entry.StepKey)
		}
		if ok && step.NextStatus != "" {
			if err := o.linear.UpdateIssueState(entry.Issue.ID, step.NextStatus); err != nil {
				log.Printf("[%s] update to next status %q: %v", entry.Identifier, step.NextStatus, err)
			} else {
				log.Printf("[%s] moved to %q", entry.Identifier, step.NextStatus)
			}
		} else {
			log.Printf("[%s] no next status transition (found=%v, nextStatus=%q)", entry.Identifier, ok, step.NextStatus)
		}

		delete(o.claimed, result.IssueID)
	} else {
		log.Printf("[%s] agent failed (attempt %d): %v", entry.Identifier, entry.Attempt, result.Err)

		// revert issue status back to trigger status so it remains eligible for retry
		step, ok := o.workflow.Config.Tracker.Steps[entry.StepKey]
		if ok && step.ProgressStatus != "" {
			if err := o.linear.UpdateIssueState(entry.Issue.ID, step.TriggerStatus); err != nil {
				log.Printf("[%s] revert to trigger status %q: %v", entry.Identifier, step.TriggerStatus, err)
			} else {
				log.Printf("[%s] reverted to %q", entry.Identifier, step.TriggerStatus)
			}
		}

		delay := retryDelay(entry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(entry.IssueID, entry.Identifier, entry.Attempt+1, delay, result.Err)
	}
}

func (o *Orchestrator) scheduleRetry(issueID, identifier string, attempt int, delay time.Duration, err error) {
	if existing, ok := o.retries[issueID]; ok {
		existing.Timer.Stop()
	}

	timer := time.AfterFunc(delay, func() {
		o.retryCh <- issueID
	})

	o.retries[issueID] = &RetryEntry{
		IssueID:    issueID,
		Identifier: identifier,
		Attempt:    attempt,
		DueAt:      time.Now().Add(delay),
		Error:      err,
		Timer:      timer,
	}

	if err != nil {
		log.Printf("[%s] retry scheduled in %s (attempt %d)", identifier, delay.Round(time.Second), attempt)
	}
}

func (o *Orchestrator) handleRetry(ctx context.Context, issueID string) {
	retry, ok := o.retries[issueID]
	if !ok {
		return
	}
	delete(o.retries, issueID)

	cfg := o.workflow.Config.Tracker
	candidates, err := o.linear.FetchIssues(o.issueFilter(cfg.ActiveStates()))
	if err != nil {
		log.Printf("[%s] retry fetch: %v", retry.Identifier, err)
		delay := retryDelay(retry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(issueID, retry.Identifier, retry.Attempt+1, delay, err)
		return
	}

	var found *Issue
	for i := range candidates {
		if candidates[i].ID == issueID {
			found = &candidates[i]
			break
		}
	}

	if found == nil {
		log.Printf("[%s] issue no longer eligible, releasing claim", retry.Identifier)
		delete(o.claimed, issueID)
		return
	}

	if len(o.running) >= o.workflow.Config.Agent.MaxConcurrent {
		log.Printf("[%s] no slots available for retry, requeueing", retry.Identifier)
		delay := retryDelay(retry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(issueID, retry.Identifier, retry.Attempt+1, delay, retry.Error)
		return
	}

	o.dispatch(ctx, *found, retry.Attempt)
}

func retryDelay(attempt int, maxBackoff time.Duration) time.Duration {
	base := 10 * time.Second
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > maxBackoff {
		delay = maxBackoff
	}
	return delay
}
