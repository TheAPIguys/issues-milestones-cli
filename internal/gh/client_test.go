package gh

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestParseRepositoryRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		host string
		want string
	}{
		{name: "github shorthand", ref: "octo/demo", host: "github.com", want: "octo/demo"},
		{name: "enterprise", ref: "github.example.com/octo/demo", host: "github.example.com", want: "github.example.com/octo/demo"},
		{name: "url", ref: "https://github.com/octo/demo.git", host: "github.com", want: "octo/demo"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, err := ParseRepositoryRef(test.ref)
			if err != nil {
				t.Fatalf("ParseRepositoryRef() error = %v", err)
			}
			if repository.Host != test.host || repository.Ref != test.want {
				t.Fatalf("ParseRepositoryRef() = host %q, ref %q; want host %q, ref %q", repository.Host, repository.Ref, test.host, test.want)
			}
		})
	}
}

func TestParseRepositoryRefRejectsInvalidValues(t *testing.T) {
	for _, ref := range []string{"", "owner", "one/two/three/four", "owner/"} {
		if _, err := ParseRepositoryRef(ref); err == nil {
			t.Errorf("ParseRepositoryRef(%q) returned no error", ref)
		}
	}
}

func TestDecodePages(t *testing.T) {
	type item struct {
		Number int `json:"number"`
	}

	items, err := decodePages[item]([]byte(`[{"number":1},{"number":2}]`))
	if err != nil {
		t.Fatalf("decodePages() error = %v", err)
	}
	if got := len(items); got != 2 {
		t.Fatalf("decodePages() returned %d items, want 2", got)
	}

	items, err = decodePages[item]([]byte(`[[{"number":1}],[{"number":2}]]`))
	if err != nil {
		t.Fatalf("decodePages() paginated error = %v", err)
	}
	want := []item{{Number: 1}, {Number: 2}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("decodePages() = %#v, want %#v", items, want)
	}
}

func TestListIssuesFiltersPullRequests(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{[]byte(`[[
  {"number": 1, "title": "Issue", "state": "open", "html_url": "https://github.com/o/r/issues/1", "comments": 2, "labels": [], "pull_request": null},
  {"number": 2, "title": "Pull request", "state": "open", "html_url": "https://github.com/o/r/pull/2", "comments": 0, "labels": [], "pull_request": {"url": "https://github.com/o/r/pulls/2"}}
 ]]`)}}
	client := NewClientWithRunner(runner)
	repository, _ := ParseRepositoryRef("o/r")

	issues, err := client.listIssues(context.Background(), repository)
	if err != nil {
		t.Fatalf("listIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Fatalf("listIssues() = %#v, want only issue #1", issues)
	}
	if issues[0].CommentCount != 2 {
		t.Fatalf("comment count = %d, want 2", issues[0].CommentCount)
	}
}

func TestLoadIssueConvertsCommentsAndDependencies(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{
		[]byte(`{
  "number": 7,
  "title": "Needs work",
  "body": "Description",
  "state": "OPEN",
  "author": {"login": "owner"},
  "assignees": [{"login": "dev"}],
  "labels": [{"name": "bug", "color": "ff0000"}],
  "milestone": {"number": 1, "title": "v1", "state": "OPEN", "html_url": "https://github.com/o/r/milestone/1"},
  "createdAt": "2026-01-01T00:00:00Z",
  "updatedAt": "2026-01-02T00:00:00Z",
  "url": "https://github.com/o/r/issues/7"
}`),
		[]byte(`[[{"user": {"login": "reviewer"}, "body": "Please fix this", "created_at": "2026-01-01T00:00:00Z"}]]`),
		[]byte(`[[{"number": 3, "title": "Prerequisite", "state": "open", "html_url": "https://github.com/o/r/issues/3"}]]`),
		[]byte(`[[{"number": 9, "title": "Follow-up", "state": "open", "html_url": "https://github.com/o/r/issues/9"}]]`),
	}}
	client := NewClientWithRunner(runner)
	repository, _ := ParseRepositoryRef("o/r")

	detail, err := client.LoadIssue(context.Background(), repository, 7)
	if err != nil {
		t.Fatalf("LoadIssue() error = %v", err)
	}
	if detail.Author != "owner" || len(detail.Assignees) != 1 || detail.Assignees[0] != "dev" {
		t.Fatalf("detail metadata = %#v", detail)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Author != "reviewer" {
		t.Fatalf("comments = %#v", detail.Comments)
	}
	if detail.Comments[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("comment created time = %q", detail.Comments[0].CreatedAt)
	}
	if len(detail.BlockedBy) != 1 || len(detail.Blocking) != 1 {
		t.Fatalf("dependencies = blocked by %#v, blocking %#v", detail.BlockedBy, detail.Blocking)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("LoadIssue() made %d calls, want 4: %#v", len(runner.calls), runner.calls)
	}
	if runner.calls[1][0] != "api" || runner.calls[1][len(runner.calls[1])-1] != "repos/o/r/issues/7/comments?per_page=100" {
		t.Fatalf("comments call = %#v", runner.calls[1])
	}
	if runner.calls[2][len(runner.calls[2])-1] != "repos/o/r/issues/7/dependencies/blocked_by?per_page=100" {
		t.Fatalf("blocked-by call = %#v", runner.calls[2])
	}
	if runner.calls[3][len(runner.calls[3])-1] != "repos/o/r/issues/7/dependencies/blocking?per_page=100" {
		t.Fatalf("blocking call = %#v", runner.calls[3])
	}
}

func TestFriendlyErrorRecognizesMissingCLI(t *testing.T) {
	err := &CommandError{Err: exec.ErrNotFound}
	if got := FriendlyError(err); got == "" || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("FriendlyError() = %q", got)
	}
}

func TestCreateWorktreeUsesIssueNumber(t *testing.T) {
	runner := &fakeRunner{}
	client := NewClientWithRunner(runner)

	if err := client.CreateWorktree(context.Background(), 42); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	want := []string{"git-wt", "switch", "--create", "i-42"}
	if !reflect.DeepEqual(runner.commandCalls, [][]string{want}) {
		t.Fatalf("worktree command = %#v, want %#v", runner.commandCalls, [][]string{want})
	}
}

type fakeRunner struct {
	outputs      [][]byte
	calls        [][]string
	commandCalls [][]string
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(r.outputs) == 0 {
		return nil, errors.New("fake runner has no output")
	}
	output := r.outputs[0]
	r.outputs = r.outputs[1:]
	return output, nil
}

func (r *fakeRunner) RunCommand(_ context.Context, executable string, args ...string) ([]byte, error) {
	r.commandCalls = append(r.commandCalls, append([]string{executable}, args...))
	return nil, nil
}

func (r *fakeRunner) RunInteractive(context.Context, ...string) error {
	return nil
}

func TestRepositoryResponseRoundTrip(t *testing.T) {
	data, err := json.Marshal(repositoryResponse{NameWithOwner: "o/r", URL: "https://github.com/o/r"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded repositoryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.NameWithOwner != "o/r" {
		t.Fatalf("decoded repository = %#v", decoded)
	}
}
