package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LinearClient is a thin GraphQL client for Linear.
type LinearClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewLinearClient creates a new Linear API client.
func NewLinearClient(endpoint, apiKey string) *LinearClient {
	return &LinearClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
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

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

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
	BranchName string `json:"branchName"`
	Labels     struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

func (n issueNode) toIssue() Issue {
	labels := make([]string, len(n.Labels.Nodes))
	for i, l := range n.Labels.Nodes {
		labels[i] = strings.ToLower(l.Name)
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
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
	}
}

// IssueFilter holds parameters for querying issues.
type IssueFilter struct {
	ProjectSlug   string
	States        []string
	Labels        []string // if non-empty, filter issues that have all these labels
	AssigneeEmail string   // if non-empty, filter by assignee email
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
	if filter.AssigneeEmail != "" {
		varDecls = append(varDecls, "$email: String!")
		filterClauses = append(filterClauses, "assignee: { email: { eq: $email } }")
		vars["email"] = filter.AssigneeEmail
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

		if !result.Issues.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Issues.PageInfo.EndCursor
	}

	return allIssues, nil
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
