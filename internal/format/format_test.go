package format

import (
	"strings"
	"testing"

	"github.com/TheAPIguys/issues-milestones-cli/internal/gh"
)

func TestIssueMarkdownIncludesIssueDetailsAndComments(t *testing.T) {
	repository, _ := gh.ParseRepositoryRef("octo/demo")
	detail := gh.IssueDetail{
		IssueSummary: gh.IssueSummary{
			Number:       12,
			Title:        "Fix login",
			State:        "OPEN",
			Author:       "alice",
			URL:          "https://github.com/octo/demo/issues/12",
			CommentCount: 1,
			Milestone:    &gh.MilestoneRef{Title: "v1"},
		},
		Body:      "The login flow is broken.",
		BlockedBy: []gh.IssueReference{{Number: 4, Title: "Auth setup"}},
		Comments:  []gh.Comment{{Author: "bob", Body: "I can reproduce this."}},
	}

	markdown := IssueMarkdown(repository, detail, true)
	for _, expected := range []string{
		"# #12 Fix login",
		"Repository: `octo/demo`",
		"Milestone: v1",
		"The login flow is broken.",
		"Blocked by #4 Auth setup",
		"@bob",
		"I can reproduce this.",
	} {
		if !strings.Contains(markdown, expected) {
			t.Errorf("IssueMarkdown() does not contain %q:\n%s", expected, markdown)
		}
	}
}

func TestIssueMarkdownCanCollapseComments(t *testing.T) {
	repository, _ := gh.ParseRepositoryRef("octo/demo")
	detail := gh.IssueDetail{IssueSummary: gh.IssueSummary{Number: 1, Title: "Issue"}, Comments: []gh.Comment{{Body: "secret"}}}
	markdown := IssueMarkdown(repository, detail, false)
	if strings.Contains(markdown, "secret") || strings.Contains(markdown, "## Comments") {
		t.Fatalf("collapsed markdown contains comments:\n%s", markdown)
	}
}
