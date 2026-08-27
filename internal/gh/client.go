package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
	RunCommand(context.Context, string, ...string) ([]byte, error)
	RunInteractive(context.Context, ...string) error
}

type commandRunner struct {
	directory string
}

func (r commandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return r.run(ctx, "gh", args...)
}

func (r commandRunner) RunCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return r.run(ctx, executable, args...)
}

func (r commandRunner) run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	if r.directory != "" {
		command.Dir = r.directory
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, &CommandError{
			Executable: executable,
			Args:       append([]string(nil), args...),
			Err:        err,
			Stderr:     strings.TrimSpace(stderr.String()),
		}
	}
	return stdout.Bytes(), nil
}

func (r commandRunner) RunInteractive(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "gh", args...)
	if r.directory != "" {
		command.Dir = r.directory
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return &CommandError{Executable: "gh", Args: append([]string(nil), args...), Err: err}
	}
	return nil
}

type CommandError struct {
	Executable string
	Args       []string
	Err        error
	Stderr     string
}

func (e *CommandError) Error() string {
	executable := e.Executable
	if executable == "" {
		executable = "gh"
	}
	command := executable + " " + strings.Join(e.Args, " ")
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s", command, e.Stderr)
	}
	return fmt.Sprintf("%s: %v", command, e.Err)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, exec.ErrNotFound) {
		var commandErr *CommandError
		if errors.As(err, &commandErr) && commandErr.Executable == "git-wt" {
			return "git-wt was not found on PATH. Install it before creating worktrees."
		}
		return "gh CLI was not found on PATH. Install it from https://cli.github.com/"
	}

	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.Stderr != "" {
		return commandErr.Stderr
	}
	return err.Error()
}

type Client struct {
	runner Runner
}

func NewClient(directory string) *Client {
	return &Client{runner: commandRunner{directory: directory}}
}

func NewClientWithRunner(runner Runner) *Client {
	return &Client{runner: runner}
}

func (c *Client) ResolveRepository(ctx context.Context, explicit, last string) (Repository, []Repository, error) {
	if strings.TrimSpace(explicit) != "" {
		repository, err := ParseRepositoryRef(explicit)
		return repository, nil, err
	}

	if repository, err := c.CurrentRepository(ctx); err == nil {
		return repository, nil, nil
	}

	if strings.TrimSpace(last) != "" {
		repository, err := ParseRepositoryRef(last)
		if err == nil {
			return repository, nil, nil
		}
	}

	repositories, err := c.ListRepositories(ctx)
	if err != nil {
		return Repository{}, nil, err
	}
	if len(repositories) == 0 {
		return Repository{}, nil, errors.New("no repositories are available to the authenticated gh account")
	}
	return Repository{}, repositories, nil
}

func (c *Client) CurrentRepository(ctx context.Context) (Repository, error) {
	data, err := c.runner.Run(ctx, "repo", "view", "--json", "nameWithOwner,url,description,isPrivate")
	if err != nil {
		return Repository{}, err
	}

	var response repositoryResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return Repository{}, fmt.Errorf("decode current repository: %w", err)
	}
	return repositoryFromResponse(response)
}

func (c *Client) ListRepositories(ctx context.Context) ([]Repository, error) {
	data, err := c.runner.Run(ctx, "repo", "list", "--limit", "100", "--json", "nameWithOwner,url,description,isPrivate")
	if err != nil {
		return nil, err
	}

	var responses []repositoryResponse
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, fmt.Errorf("decode repositories: %w", err)
	}

	repositories := make([]Repository, 0, len(responses))
	for _, response := range responses {
		repository, err := repositoryFromResponse(response)
		if err != nil {
			continue
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func (c *Client) LoadSummary(ctx context.Context, repository Repository) (Summary, error) {
	milestones, err := c.listMilestones(ctx, repository)
	if err != nil {
		return Summary{}, fmt.Errorf("load milestones: %w", err)
	}
	issues, err := c.listIssues(ctx, repository)
	if err != nil {
		return Summary{}, fmt.Errorf("load issues: %w", err)
	}
	return Summary{Repo: repository, Milestones: milestones, Issues: issues}, nil
}

func (c *Client) listMilestones(ctx context.Context, repository Repository) ([]Milestone, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/milestones?state=open&per_page=100", repository.Owner, repository.Name)
	data, err := c.api(ctx, repository, endpoint)
	if err != nil {
		return nil, err
	}

	items, err := decodePages[apiMilestone](data)
	if err != nil {
		return nil, fmt.Errorf("decode milestones: %w", err)
	}
	milestones := make([]Milestone, 0, len(items))
	for _, item := range items {
		milestones = append(milestones, Milestone{
			Number:       item.Number,
			Title:        item.Title,
			Description:  item.Description,
			State:        item.State,
			DueOn:        item.DueOn,
			OpenIssues:   item.OpenIssues,
			ClosedIssues: item.ClosedIssues,
			URL:          item.HTMLURL,
		})
	}
	return milestones, nil
}

func (c *Client) listIssues(ctx context.Context, repository Repository) ([]IssueSummary, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/issues?state=open&per_page=100", repository.Owner, repository.Name)
	data, err := c.api(ctx, repository, endpoint)
	if err != nil {
		return nil, err
	}

	items, err := decodePages[apiIssue](data)
	if err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}
	issues := make([]IssueSummary, 0, len(items))
	for _, item := range items {
		if len(item.PullRequest) > 0 && string(item.PullRequest) != "null" {
			continue
		}
		issues = append(issues, item.summary())
	}
	return issues, nil
}

func (c *Client) LoadIssue(ctx context.Context, repository Repository, number int) (IssueDetail, error) {
	data, err := c.runner.Run(
		ctx,
		"issue", "view", strconv.Itoa(number),
		"--repo", repository.Ref,
		"--json", "number,title,body,state,author,assignees,labels,milestone,comments,createdAt,updatedAt,url",
	)
	if err != nil {
		return IssueDetail{}, err
	}

	var response issueViewResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return IssueDetail{}, fmt.Errorf("decode issue #%d: %w", number, err)
	}
	comments, err := c.listIssueComments(ctx, repository, number)
	if err != nil {
		return IssueDetail{}, fmt.Errorf("load comments for issue #%d: %w", number, err)
	}
	dependencies, err := c.LoadIssueDependencies(ctx, repository, number)
	if err != nil {
		return IssueDetail{}, err
	}

	labels := make([]Label, 0, len(response.Labels))
	for _, label := range response.Labels {
		labels = append(labels, Label{Name: label.Name, Color: label.Color})
	}
	assignees := make([]string, 0, len(response.Assignees))
	for _, assignee := range response.Assignees {
		assignees = append(assignees, assignee.Login)
	}

	summary := IssueSummary{
		Number:       response.Number,
		Title:        response.Title,
		State:        response.State,
		Author:       response.Author.Login,
		URL:          response.URL,
		Labels:       labels,
		Milestone:    response.Milestone.reference(),
		CommentCount: len(comments),
		CreatedAt:    response.CreatedAt,
		UpdatedAt:    response.UpdatedAt,
	}
	summary.BlockedByCount = len(dependencies.BlockedBy)
	summary.BlockingCount = len(dependencies.Blocking)

	return IssueDetail{
		IssueSummary: summary,
		Body:         response.Body,
		Assignees:    assignees,
		Comments:     convertComments(comments),
		BlockedBy:    dependencies.BlockedBy,
		Blocking:     dependencies.Blocking,
	}, nil
}

func (c *Client) LoadIssueDependencies(ctx context.Context, repository Repository, number int) (IssueDependencies, error) {
	blockedBy, err := c.listIssueDependencies(ctx, repository, number, "blocked_by")
	if err != nil {
		return IssueDependencies{}, fmt.Errorf("load issues blocking #%d: %w", number, err)
	}
	blocking, err := c.listIssueDependencies(ctx, repository, number, "blocking")
	if err != nil {
		return IssueDependencies{}, fmt.Errorf("load issues blocked by #%d: %w", number, err)
	}
	return IssueDependencies{BlockedBy: blockedBy, Blocking: blocking}, nil
}

func (c *Client) OpenIssue(ctx context.Context, repository Repository, number int) error {
	return c.runner.RunInteractive(ctx, "issue", "view", strconv.Itoa(number), "--repo", repository.Ref, "--web")
}

func (c *Client) CreateWorktree(ctx context.Context, number int) error {
	_, err := c.runner.RunCommand(ctx, "git-wt", "switch", "--create", fmt.Sprintf("i-%d", number))
	return err
}

func (c *Client) listIssueComments(ctx context.Context, repository Repository, number int) ([]apiComment, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100", repository.Owner, repository.Name, number)
	data, err := c.api(ctx, repository, endpoint)
	if err != nil {
		return nil, err
	}
	return decodePages[apiComment](data)
}

func (c *Client) listIssueDependencies(ctx context.Context, repository Repository, number int, direction string) ([]IssueReference, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/issues/%d/dependencies/%s?per_page=100", repository.Owner, repository.Name, number, direction)
	data, err := c.api(ctx, repository, endpoint)
	if err != nil {
		return nil, err
	}
	items, err := decodePages[apiIssue](data)
	if err != nil {
		return nil, fmt.Errorf("decode dependencies: %w", err)
	}
	dependencies := make([]IssueReference, 0, len(items))
	for _, item := range items {
		dependencies = append(dependencies, item.reference())
	}
	return dependencies, nil
}

func (c *Client) api(ctx context.Context, repository Repository, endpoint string) ([]byte, error) {
	args := []string{"api"}
	if repository.Host != "" && repository.Host != "github.com" {
		args = append(args, "--hostname", repository.Host)
	}
	args = append(args, "--paginate", "--slurp", endpoint)
	return c.runner.Run(ctx, args...)
}

func decodePages[T any](data []byte) ([]T, error) {
	var pages [][]T
	if err := json.Unmarshal(data, &pages); err == nil {
		items := make([]T, 0)
		for _, page := range pages {
			items = append(items, page...)
		}
		return items, nil
	}

	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

type repositoryResponse struct {
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
	Description   string `json:"description"`
	Private       bool   `json:"isPrivate"`
}

func repositoryFromResponse(response repositoryResponse) (Repository, error) {
	ref := response.NameWithOwner
	if host := strings.TrimSpace(os.Getenv("GH_HOST")); host != "" && strings.Count(ref, "/") == 1 {
		ref = host + "/" + ref
	}
	repository, err := ParseRepositoryRef(ref)
	if err != nil {
		return Repository{}, err
	}
	repository.URL = response.URL
	repository.Description = response.Description
	repository.Private = response.Private
	return repository, nil
}

type apiMilestone struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	DueOn        string `json:"due_on"`
	OpenIssues   int    `json:"open_issues"`
	ClosedIssues int    `json:"closed_issues"`
	HTMLURL      string `json:"html_url"`
}

type apiMilestoneRef struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	URL     string `json:"url"`
	HTMLURL string `json:"html_url"`
}

func (m *apiMilestoneRef) reference() *MilestoneRef {
	if m == nil {
		return nil
	}
	return &MilestoneRef{Number: m.Number, Title: m.Title, State: m.State, URL: firstNonEmpty(m.URL, m.HTMLURL)}
}

type apiUser struct {
	Login string `json:"login"`
}

type apiLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type apiDependencySummary struct {
	BlockedBy int `json:"blocked_by"`
	Blocking  int `json:"blocking"`
}

type apiIssue struct {
	Number                   int                   `json:"number"`
	Title                    string                `json:"title"`
	State                    string                `json:"state"`
	User                     *apiUser              `json:"user"`
	HTMLURL                  string                `json:"html_url"`
	Labels                   []apiLabel            `json:"labels"`
	Milestone                *apiMilestoneRef      `json:"milestone"`
	Comments                 int                   `json:"comments"`
	IssueDependenciesSummary *apiDependencySummary `json:"issue_dependencies_summary"`
	CreatedAt                string                `json:"created_at"`
	UpdatedAt                string                `json:"updated_at"`
	PullRequest              json.RawMessage       `json:"pull_request"`
}

func (i apiIssue) summary() IssueSummary {
	labels := make([]Label, 0, len(i.Labels))
	for _, label := range i.Labels {
		labels = append(labels, Label{Name: label.Name, Color: label.Color})
	}
	var author string
	if i.User != nil {
		author = i.User.Login
	}
	var blockedByCount, blockingCount int
	if i.IssueDependenciesSummary != nil {
		blockedByCount = i.IssueDependenciesSummary.BlockedBy
		blockingCount = i.IssueDependenciesSummary.Blocking
	}
	return IssueSummary{
		Number:         i.Number,
		Title:          i.Title,
		State:          i.State,
		Author:         author,
		URL:            i.HTMLURL,
		Labels:         labels,
		Milestone:      i.Milestone.reference(),
		CommentCount:   i.Comments,
		BlockedByCount: blockedByCount,
		BlockingCount:  blockingCount,
		CreatedAt:      i.CreatedAt,
		UpdatedAt:      i.UpdatedAt,
	}
}

func (i apiIssue) reference() IssueReference {
	return IssueReference{Number: i.Number, Title: i.Title, State: i.State, URL: i.HTMLURL}
}

type issueViewResponse struct {
	Number    int              `json:"number"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	State     string           `json:"state"`
	Author    apiUser          `json:"author"`
	Assignees []apiUser        `json:"assignees"`
	Labels    []apiLabel       `json:"labels"`
	Milestone *apiMilestoneRef `json:"milestone"`
	CreatedAt string           `json:"createdAt"`
	UpdatedAt string           `json:"updatedAt"`
	URL       string           `json:"url"`
}

type apiComment struct {
	Author         apiUser `json:"author"`
	User           apiUser `json:"user"`
	Body           string  `json:"body"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	CreatedAtSnake string  `json:"created_at"`
	UpdatedAtSnake string  `json:"updated_at"`
}

func convertComments(comments []apiComment) []Comment {
	converted := make([]Comment, 0, len(comments))
	for _, comment := range comments {
		author := comment.Author.Login
		if author == "" {
			author = comment.User.Login
		}
		converted = append(converted, Comment{
			Author:    author,
			Body:      comment.Body,
			CreatedAt: firstNonEmpty(comment.CreatedAt, comment.CreatedAtSnake),
			UpdatedAt: firstNonEmpty(comment.UpdatedAt, comment.UpdatedAtSnake),
		})
	}
	return converted
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
