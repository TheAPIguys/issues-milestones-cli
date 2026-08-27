package gh

import (
	"fmt"
	"net/url"
	"strings"
)

// Repository is the normalized repository reference used by the application.
type Repository struct {
	Host          string `json:"host"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
	Ref           string `json:"ref"`
	URL           string `json:"url"`
	Description   string `json:"description"`
	Private       bool   `json:"isPrivate"`
}

func (r Repository) String() string {
	if r.Ref != "" {
		return r.Ref
	}
	return r.NameWithOwner
}

// ParseRepositoryRef accepts OWNER/REPO, HOST/OWNER/REPO, and GitHub URLs.
func ParseRepositoryRef(ref string) (Repository, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Repository{}, fmt.Errorf("repository is empty")
	}

	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		parsed, err := url.Parse(ref)
		if err != nil {
			return Repository{}, fmt.Errorf("invalid repository URL: %w", err)
		}
		ref = strings.Trim(parsed.Path, "/")
		if parsed.Host != "" {
			ref = parsed.Host + "/" + ref
		}
	}

	ref = strings.TrimSuffix(ref, ".git")
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	var host, owner, name string
	switch len(parts) {
	case 2:
		host, owner, name = "github.com", parts[0], parts[1]
	case 3:
		host, owner, name = parts[0], parts[1], parts[2]
	default:
		return Repository{}, fmt.Errorf("repository must be OWNER/REPO or HOST/OWNER/REPO")
	}
	if owner == "" || name == "" || strings.ContainsAny(owner+name, " \t\r\n") {
		return Repository{}, fmt.Errorf("invalid repository %q", ref)
	}

	nameWithOwner := owner + "/" + name
	canonicalRef := nameWithOwner
	if host != "github.com" {
		canonicalRef = host + "/" + nameWithOwner
	}

	return Repository{
		Host:          host,
		Owner:         owner,
		Name:          name,
		NameWithOwner: nameWithOwner,
		Ref:           canonicalRef,
		URL:           "https://" + host + "/" + nameWithOwner,
	}, nil
}

type Summary struct {
	Repo       Repository
	Milestones []Milestone
	Issues     []IssueSummary
}

type Milestone struct {
	Number       int
	Title        string
	Description  string
	State        string
	DueOn        string
	OpenIssues   int
	ClosedIssues int
	URL          string
}

type Label struct {
	Name  string
	Color string
}

type MilestoneRef struct {
	Number int
	Title  string
	State  string
	URL    string
}

type IssueSummary struct {
	Number         int
	Title          string
	State          string
	Author         string
	URL            string
	Labels         []Label
	Milestone      *MilestoneRef
	CommentCount   int
	BlockedByCount int
	BlockingCount  int
	CreatedAt      string
	UpdatedAt      string
}

type IssueDetail struct {
	IssueSummary
	Body      string
	Assignees []string
	Comments  []Comment
	BlockedBy []IssueReference
	Blocking  []IssueReference
}

type IssueDependencies struct {
	BlockedBy []IssueReference
	Blocking  []IssueReference
}

type Comment struct {
	Author    string
	Body      string
	CreatedAt string
	UpdatedAt string
}

type IssueReference struct {
	Number int
	Title  string
	State  string
	URL    string
}
