package daemon

import (
	"context"
	"fmt"
	"log"
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

	o.startupCleanup()

	// Immediate first tick
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
		ProjectSlug:   cfg.ProjectSlug,
		States:        states,
		Labels:        cfg.Labels,
		AssigneeEmail: cfg.AssigneeEmail,
	}
}

func (o *Orchestrator) startupCleanup() {
	cfg := o.workflow.Config.Tracker
	issues, err := o.linear.FetchIssues(o.issueFilter(cfg.TerminalStates))
	if err != nil {
		log.Printf("startup cleanup: fetch terminal issues: %v", err)
		return
	}

	for _, issue := range issues {
		branch := issueBranch(issue)
		repoName := filepath.Base(o.repoRoot)
		spaceName := fmt.Sprintf("%s-%s", repoName, spaces.SlugifyBranch(branch))
		worktreePath := filepath.Join(o.destDir, spaceName)

		if _, statErr := filepath.Abs(worktreePath); statErr == nil {
			log.Printf("[%s] cleaning up terminal workspace", issue.Identifier)
			CleanupWorkspace(issue, o.repoRoot, o.destDir)
		}
	}
}

func (o *Orchestrator) tick(ctx context.Context) {
	// Reload workflow
	wf, err := LoadWorkflow(o.workflowPath)
	if err != nil {
		log.Printf("reload workflow: %v (keeping last good config)", err)
	} else {
		o.workflow = wf
		o.linear = NewLinearClient(wf.Config.Tracker.Endpoint, wf.Config.Tracker.APIKey)
	}

	// Reconcile running issues
	o.reconcile(ctx)

	// Fetch candidates
	cfg := o.workflow.Config.Tracker
	candidates, err := o.linear.FetchIssues(o.issueFilter(cfg.ActiveStates))
	if err != nil {
		log.Printf("fetch candidates: %v", err)
		return
	}

	// Sort: priority asc (nil last), created_at asc, identifier asc
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

	// Dispatch eligible
	for _, issue := range candidates {
		if o.claimed[issue.ID] {
			continue
		}
		if len(o.running) >= o.workflow.Config.Agent.MaxConcurrent {
			break
		}
		o.dispatch(ctx, issue, 1)
	}
}

func (o *Orchestrator) reconcile(ctx context.Context) {
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
		terminalSet[s] = true
	}
	activeSet := make(map[string]bool, len(cfg.ActiveStates))
	for _, s := range cfg.ActiveStates {
		activeSet[s] = true
	}

	for id, entry := range o.running {
		state, found := states[id]
		if !found {
			// Issue not found — cancel
			log.Printf("[%s] issue not found during reconcile, cancelling", entry.Identifier)
			entry.Cancel()
			continue
		}

		if terminalSet[state] {
			log.Printf("[%s] issue reached terminal state %q, cancelling and cleaning up", entry.Identifier, state)
			entry.Cancel()
			CleanupWorkspace(entry.Issue, o.repoRoot, o.destDir)
			continue
		}

		if activeSet[state] {
			entry.Issue.State = state
		} else {
			// Agent moved the issue to a non-active state (e.g. "Planned", "Review").
			// Let it finish — it will exit on its own.
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
	labels := "none"
	if len(issue.Labels) > 0 {
		labels = strings.Join(issue.Labels, ", ")
	}
	log.Printf("[%s] picking up issue (attempt %d)", issue.Identifier, attempt)
	log.Printf("[%s]   title:    %s", issue.Identifier, issue.Title)
	log.Printf("[%s]   state:    %s", issue.Identifier, issue.State)
	log.Printf("[%s]   priority: %s", issue.Identifier, priorityName(issue.Priority))
	log.Printf("[%s]   labels:   %s", issue.Identifier, labels)
	log.Printf("[%s]   branch:   %s", issue.Identifier, branch)
	log.Printf("[%s]   url:      %s", issue.Identifier, issue.URL)

	workspacePath, err := EnsureWorkspace(issue, o.repoRoot, o.destDir)
	if err != nil {
		log.Printf("[%s] ensure workspace: %v", issue.Identifier, err)
		return
	}

	if err := EnsureSession(workspacePath, o.destDir); err != nil {
		log.Printf("[%s] ensure session: %v", issue.Identifier, err)
		return
	}

	attemptPtr := &attempt
	prompt, err := o.workflow.RenderPrompt(issue, attemptPtr, false)
	if err != nil {
		log.Printf("[%s] render prompt: %v", issue.Identifier, err)
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	resultCh, err := LaunchAgent(runCtx, issue, o.workflow.Config.Agent.Command, prompt, workspacePath)
	if err != nil {
		cancel()
		log.Printf("[%s] launch agent: %v", issue.Identifier, err)
		return
	}

	entry := &RunEntry{
		IssueID:    issue.ID,
		Identifier: issue.Identifier,
		Issue:      issue,
		Attempt:    attempt,
		StartedAt:  time.Now(),
		Cancel:     cancel,
		ResultCh:   resultCh,
	}

	o.running[issue.ID] = entry
	o.claimed[issue.ID] = true

	// Forward results to shared channel
	go func() {
		result := <-resultCh
		o.resultCh <- result
	}()
}

func (o *Orchestrator) dispatchStatusFix(ctx context.Context, issue Issue, attempt int) {
	log.Printf("[%s] dispatching status fix", issue.Identifier)

	workspacePath, err := EnsureWorkspace(issue, o.repoRoot, o.destDir)
	if err != nil {
		log.Printf("[%s] ensure workspace: %v", issue.Identifier, err)
		return
	}

	if err := EnsureSession(workspacePath, o.destDir); err != nil {
		log.Printf("[%s] ensure session: %v", issue.Identifier, err)
		return
	}

	prompt, err := o.workflow.RenderPrompt(issue, nil, true)
	if err != nil {
		log.Printf("[%s] render status fix prompt: %v", issue.Identifier, err)
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	resultCh, err := LaunchAgent(runCtx, issue, o.workflow.Config.Agent.Command, prompt, workspacePath)
	if err != nil {
		cancel()
		log.Printf("[%s] launch status fix agent: %v", issue.Identifier, err)
		return
	}

	entry := &RunEntry{
		IssueID:    issue.ID,
		Identifier: issue.Identifier,
		Issue:      issue,
		Attempt:    attempt,
		StatusFix:  true,
		StartedAt:  time.Now(),
		Cancel:     cancel,
		ResultCh:   resultCh,
	}

	o.running[issue.ID] = entry

	go func() {
		result := <-resultCh
		o.resultCh <- result
	}()
}

func (o *Orchestrator) handleResult(ctx context.Context, result RunResult) {
	entry, ok := o.running[result.IssueID]
	if !ok {
		return
	}
	delete(o.running, result.IssueID)

	if result.Success {
		log.Printf("[%s] agent completed successfully (attempt %d)", entry.Identifier, entry.Attempt)
		o.scheduleRetry(entry.IssueID, entry.Identifier, entry.Attempt, 1*time.Second, nil, entry.Issue.State, entry.StatusFix)
	} else {
		log.Printf("[%s] agent failed (attempt %d): %v", entry.Identifier, entry.Attempt, result.Err)
		delay := retryDelay(entry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(entry.IssueID, entry.Identifier, entry.Attempt+1, delay, result.Err, "", false)
	}
}

func (o *Orchestrator) scheduleRetry(issueID, identifier string, attempt int, delay time.Duration, err error, prevState string, statusFix bool) {
	// Cancel any existing retry for this issue
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
		StatusFix:  statusFix,
		PrevState:  prevState,
		DueAt:      time.Now().Add(delay),
		Error:      err,
		Timer:      timer,
	}

	if err != nil {
		log.Printf("[%s] retry scheduled in %s (attempt %d)", identifier, delay.Round(time.Second), attempt)
	} else {
		log.Printf("[%s] rechecking eligibility in %s", identifier, delay.Round(time.Second))
	}
}

func (o *Orchestrator) handleRetry(ctx context.Context, issueID string) {
	retry, ok := o.retries[issueID]
	if !ok {
		return
	}
	delete(o.retries, issueID)

	// Check if issue is still eligible
	cfg := o.workflow.Config.Tracker
	candidates, err := o.linear.FetchIssues(o.issueFilter(cfg.ActiveStates))
	if err != nil {
		log.Printf("[%s] retry fetch: %v", retry.Identifier, err)
		// Re-schedule with incremented attempt
		delay := retryDelay(retry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(issueID, retry.Identifier, retry.Attempt+1, delay, err, "", false)
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

	if retry.Error == nil && retry.PrevState != "" && found.State == retry.PrevState {
		if retry.StatusFix {
			log.Printf("[%s] status fix did not change state (%s), releasing claim", retry.Identifier, found.State)
			delete(o.claimed, issueID)
			return
		}
		log.Printf("[%s] state unchanged after success (%s), dispatching status fix", retry.Identifier, found.State)
		o.dispatchStatusFix(ctx, *found, retry.Attempt)
		return
	}

	if len(o.running) >= o.workflow.Config.Agent.MaxConcurrent {
		log.Printf("[%s] no slots available for retry, requeueing", retry.Identifier)
		delay := retryDelay(retry.Attempt, o.workflow.Config.Agent.MaxRetryBackoff)
		o.scheduleRetry(issueID, retry.Identifier, retry.Attempt+1, delay, retry.Error, "", false)
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
