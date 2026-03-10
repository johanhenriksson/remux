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

// FetchCandidates returns issues matching the given project and active states.
func (c *LinearClient) FetchCandidates(projectSlug string, activeStates []string) ([]Issue, error) {
	return c.fetchByStates(projectSlug, activeStates)
}

// FetchByStates returns issues matching the given project and states.
func (c *LinearClient) FetchByStates(projectSlug string, states []string) ([]Issue, error) {
	return c.fetchByStates(projectSlug, states)
}

func (c *LinearClient) fetchByStates(projectSlug string, states []string) ([]Issue, error) {
	query := fmt.Sprintf(`
		query($slug: String!, $states: [String!]!, $cursor: String) {
			issues(
				filter: {
					project: { slugId: { eq: $slug } }
					state: { name: { in: $states } }
				}
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
	`, issueFields)

	var allIssues []Issue
	var cursor *string

	for {
		vars := map[string]any{
			"slug":   projectSlug,
			"states": states,
			"cursor": cursor,
		}

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

// FetchIssueStatesByIDs returns a map of issue ID to current state name.
func (c *LinearClient) FetchIssueStatesByIDs(ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}

	query := `
		query($ids: [ID!]!) {
			nodes(ids: $ids) {
				... on Issue {
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
		Nodes []struct {
			ID    string `json:"id"`
			State *struct {
				Name string `json:"name"`
			} `json:"state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal nodes: %w", err)
	}

	states := make(map[string]string, len(result.Nodes))
	for _, n := range result.Nodes {
		if n.ID != "" && n.State != nil {
			states[n.ID] = n.State.Name
		}
	}
	return states, nil
}
