package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// LinearClient is a thin GraphQL client for Linear.
type LinearClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
	debug    bool
}

func (c *LinearClient) debugf(format string, args ...any) {
	if c.debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// NewLinearClient creates a new Linear API client.
func NewLinearClient(endpoint, apiKey string, debug bool) *LinearClient {
	return &LinearClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
		debug:    debug,
	}
}

type graphQLRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *LinearClient) do(query string, variables any) (json.RawMessage, error) {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	c.debugf("graphql request: %s", firstQueryLine(query))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	c.debugf("graphql response: status=%d size=%d", resp.StatusCode, len(respBody))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear API error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	return gqlResp.Data, nil
}

func firstQueryLine(q string) string {
	q = strings.TrimSpace(q)
	if idx := strings.IndexByte(q, '{'); idx >= 0 {
		q = strings.TrimSpace(q[:idx+1]) + "..."
	}
	return q
}

const issueFields = `
	id
	identifier
	title
	description
	priority
	url
	createdAt
	updatedAt
	state { name }
	branchName
	labels { nodes { name } }
	projectMilestone { name }
	creator { id }
`

type issueNode struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    *int      `json:"priority"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	BranchName       string `json:"branchName"`
	Labels           struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	ProjectMilestone *struct {
		Name string `json:"name"`
	} `json:"projectMilestone"`
	Creator *struct {
		ID string `json:"id"`
	} `json:"creator"`
}

func (n issueNode) toIssue() Issue {
	labels := make([]string, len(n.Labels.Nodes))
	for i, l := range n.Labels.Nodes {
		labels[i] = strings.ToLower(l.Name)
	}
	var milestone string
	if n.ProjectMilestone != nil {
		milestone = n.ProjectMilestone.Name
	}
	var creatorID string
	if n.Creator != nil {
		creatorID = n.Creator.ID
	}
	return Issue{
		ID:          n.ID,
		Identifier:  n.Identifier,
		Title:       n.Title,
		Description: n.Description,
		Priority:    n.Priority,
		State:       n.State.Name,
		BranchName:  n.BranchName,
		URL:         n.URL,
		Labels:      labels,
		Milestone:   milestone,
		CreatorID:   creatorID,
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
	}
}

// IssueFilter holds parameters for querying issues.
type IssueFilter struct {
	ProjectSlug string
	States      []string
	Labels      []string // if non-empty, filter issues that have all these labels
	AssigneeID  string   // if non-empty, filter by assignee user ID
	CreatorID   string   // if non-empty, filter by creator user ID
}

// FetchIssues returns issues matching the given filter.
func (c *LinearClient) FetchIssues(filter IssueFilter) ([]Issue, error) {
	// Build variable declarations and filter clauses dynamically
	varDecls := []string{"$slug: String!", "$cursor: String"}
	filterClauses := []string{
		"project: { slugId: { eq: $slug } }",
	}
	vars := map[string]any{
		"slug": filter.ProjectSlug,
	}

	if len(filter.States) > 0 {
		varDecls = append(varDecls, "$states: [String!]!")
		filterClauses = append(filterClauses, "state: { name: { in: $states } }")
		vars["states"] = filter.States
	}

	if len(filter.Labels) > 0 {
		varDecls = append(varDecls, "$labels: [String!]!")
		filterClauses = append(filterClauses, "labels: { some: { name: { in: $labels } } }")
		vars["labels"] = filter.Labels
	}
	if filter.AssigneeID != "" {
		varDecls = append(varDecls, "$assigneeId: ID!")
		filterClauses = append(filterClauses, "assignee: { id: { eq: $assigneeId } }")
		vars["assigneeId"] = filter.AssigneeID
	}
	if filter.CreatorID != "" {
		varDecls = append(varDecls, "$creatorId: ID!")
		filterClauses = append(filterClauses, "creator: { id: { eq: $creatorId } }")
		vars["creatorId"] = filter.CreatorID
	}

	query := fmt.Sprintf(`
		query(%s) {
			issues(
				filter: { %s }
				first: 50
				after: $cursor
			) {
				nodes { %s }
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
	`, strings.Join(varDecls, ", "), strings.Join(filterClauses, "\n\t\t\t\t\t"), issueFields)

	var allIssues []Issue
	var cursor *string

	for {
		vars["cursor"] = cursor

		data, err := c.do(query, vars)
		if err != nil {
			return nil, err
		}

		var result struct {
			Issues struct {
				Nodes    []issueNode `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("unmarshal issues: %w", err)
		}

		for _, n := range result.Issues.Nodes {
			allIssues = append(allIssues, n.toIssue())
		}
		c.debugf("fetch issues: page returned %d issues (total so far: %d)", len(result.Issues.Nodes), len(allIssues))

		if !result.Issues.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Issues.PageInfo.EndCursor
		c.debugf("fetch issues: fetching next page (cursor=%s)", *cursor)
	}

	return allIssues, nil
}

// FetchViewerID returns the user ID of the authenticated API user.
func (c *LinearClient) FetchViewerID() (string, error) {
	data, err := c.do(`query { viewer { id } }`, nil)
	if err != nil {
		return "", fmt.Errorf("fetch viewer: %w", err)
	}

	var result struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("unmarshal viewer: %w", err)
	}
	if result.Viewer.ID == "" {
		return "", fmt.Errorf("viewer ID is empty")
	}
	return result.Viewer.ID, nil
}

// CreateComment creates a comment on the given issue and returns the comment ID.
func (c *LinearClient) CreateComment(issueID, body string) (string, error) {
	query := `
		mutation($issueId: String!, $body: String!) {
			commentCreate(input: { issueId: $issueId, body: $body }) {
				comment { id }
			}
		}
	`
	data, err := c.do(query, map[string]any{"issueId": issueID, "body": body})
	if err != nil {
		return "", err
	}

	var result struct {
		CommentCreate struct {
			Comment struct {
				ID string `json:"id"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("unmarshal comment: %w", err)
	}
	return result.CommentCreate.Comment.ID, nil
}

// UpdateComment updates the body of an existing comment.
func (c *LinearClient) UpdateComment(commentID, body string) error {
	query := `
		mutation($id: String!, $body: String!) {
			commentUpdate(id: $id, input: { body: $body }) {
				comment { id }
			}
		}
	`
	_, err := c.do(query, map[string]any{"id": commentID, "body": body})
	return err
}

// UpdateIssueState moves an issue to the given state by name.
// Resolves the state name to an ID by querying the issue's team workflow states.
func (c *LinearClient) UpdateIssueState(issueID, stateName string) error {
	stateQuery := `
		query($issueId: String!) {
			issue(id: $issueId) {
				team {
					states { nodes { id name } }
				}
			}
		}
	`
	data, err := c.do(stateQuery, map[string]any{"issueId": issueID})
	if err != nil {
		return fmt.Errorf("fetch team states: %w", err)
	}

	var stateResult struct {
		Issue struct {
			Team struct {
				States struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &stateResult); err != nil {
		return fmt.Errorf("unmarshal team states: %w", err)
	}

	var stateID string
	for _, s := range stateResult.Issue.Team.States.Nodes {
		if strings.EqualFold(s.Name, stateName) {
			stateID = s.ID
			break
		}
	}
	if stateID == "" {
		return fmt.Errorf("state %q not found in team workflow", stateName)
	}

	updateQuery := `
		mutation($issueId: String!, $stateId: String!) {
			issueUpdate(id: $issueId, input: { stateId: $stateId }) {
				issue { id }
			}
		}
	`
	_, err = c.do(updateQuery, map[string]any{"issueId": issueID, "stateId": stateID})
	return err
}

// FetchIssueStatesByIDs returns a map of issue ID to current state name.
func (c *LinearClient) FetchIssueStatesByIDs(ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}

	query := `
		query($ids: [ID!]!) {
			issues(filter: { id: { in: $ids } }) {
				nodes {
					id
					state { name }
				}
			}
		}
	`

	data, err := c.do(query, map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}

	var result struct {
		Issues struct {
			Nodes []struct {
				ID    string `json:"id"`
				State *struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal issues: %w", err)
	}

	states := make(map[string]string, len(result.Issues.Nodes))
	for _, n := range result.Issues.Nodes {
		if n.ID != "" && n.State != nil {
			states[n.ID] = n.State.Name
		}
	}
	return states, nil
}
