package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheAPIguys/issues-milestones-cli/internal/gh"
)

func TestBuildGroupsKeepsMilestonesAndUnassignedIssues(t *testing.T) {
	summary := gh.Summary{
		Milestones: []gh.Milestone{
			{Number: 2, Title: "v2"},
			{Number: 1, Title: "v1"},
		},
		Issues: []gh.IssueSummary{
			{Number: 12, Title: "closed milestone", Milestone: &gh.MilestoneRef{Number: 99, Title: "old"}},
			{Number: 10, Title: "v1 issue", Milestone: &gh.MilestoneRef{Number: 1, Title: "v1"}},
			{Number: 11, Title: "unassigned"},
		},
	}

	groups := buildGroups(summary)
	if len(groups) != 5 {
		t.Fatalf("got %d groups, want 5: %#v", len(groups), groups)
	}
	gotIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		gotIDs = append(gotIDs, group.ID)
	}
	wantIDs := []string{"all", "milestone:1", "milestone:2", "none", "other"}
	for index := range wantIDs {
		if gotIDs[index] != wantIDs[index] {
			t.Fatalf("group IDs = %v, want %v", gotIDs, wantIDs)
		}
	}
	if len(groups[3].Issues) != 1 || groups[3].Issues[0].Number != 11 {
		t.Fatalf("no milestone group = %#v", groups[3].Issues)
	}
	if len(groups[4].Issues) != 1 || groups[4].Issues[0].Number != 12 {
		t.Fatalf("other milestone group = %#v", groups[4].Issues)
	}
	for index, number := range []int{10, 11, 12} {
		if groups[0].Issues[index].Number != number {
			t.Fatalf("all issues = %#v, want ascending issue numbers", groups[0].Issues)
		}
	}
}

func TestBuildGroupsOmitsEmptyNoMilestoneGroup(t *testing.T) {
	groups := buildGroups(gh.Summary{
		Milestones: []gh.Milestone{{Number: 1, Title: "v1"}},
		Issues:     []gh.IssueSummary{{Number: 1, Milestone: &gh.MilestoneRef{Number: 1}}},
	})
	if len(groups) != 2 || groups[0].ID != "all" || groups[1].ID != "milestone:1" {
		t.Fatalf("groups = %#v", groups)
	}
}

func TestListWindowFollowsCursor(t *testing.T) {
	start, end := listWindow(10, 5, 3)
	if start != 3 || end != 6 {
		t.Fatalf("listWindow() = %d, %d; want 3, 6", start, end)
	}
}

func TestScrollWindowMovesWithOffset(t *testing.T) {
	start, end := scrollWindow(100, 1, 10)
	if start != 1 || end != 11 {
		t.Fatalf("scrollWindow() = %d, %d; want 1, 11", start, end)
	}

	start, end = scrollWindow(100, 95, 10)
	if start != 90 || end != 100 {
		t.Fatalf("scrollWindow() clamps = %d, %d; want 90, 100", start, end)
	}
}

func TestDetailKeysMoveScrollOffset(t *testing.T) {
	model := &Model{
		detail: &gh.IssueDetail{
			IssueSummary: gh.IssueSummary{Number: 42, Title: "Long issue"},
			Body:         "A long issue body.",
		},
		detailNumber: 42,
	}

	model.handleDetailKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.detailOffset != 1 {
		t.Fatalf("j offset = %d, want 1", model.detailOffset)
	}
	model.handleDetailKey(tea.KeyMsg{Type: tea.KeyDown})
	if model.detailOffset != 2 {
		t.Fatalf("down offset = %d, want 2", model.detailOffset)
	}
	model.handleDetailKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if model.detailOffset != 1 {
		t.Fatalf("k offset = %d, want 1", model.detailOffset)
	}
	model.handleDetailKey(tea.KeyMsg{Type: tea.KeyUp})
	if model.detailOffset != 0 {
		t.Fatalf("up offset = %d, want 0", model.detailOffset)
	}
}

func TestDetailViewChangesAfterOneScroll(t *testing.T) {
	repository, _ := gh.ParseRepositoryRef("octo/demo")
	model := &Model{
		screen: screenDetail,
		width:  80,
		height: 10,
		repo:   repository,
		detail: &gh.IssueDetail{
			IssueSummary: gh.IssueSummary{Number: 42, Title: "Long issue"},
			Body: strings.Join([]string{
				"First unique line.",
				"Second unique line.",
				"Third unique line.",
				"Fourth unique line.",
				"Fifth unique line.",
				"Sixth unique line.",
				"Seventh unique line.",
				"Eighth unique line.",
			}, "\n"),
		},
		detailNumber: 42,
	}

	before := model.renderDetail()
	model.handleDetailKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	after := model.renderDetail()
	if before == after {
		t.Fatal("detail view did not change after pressing j")
	}
}

func TestDetailLinesCacheTracksWidthAndComments(t *testing.T) {
	model := &Model{
		width: 80,
		detail: &gh.IssueDetail{
			IssueSummary: gh.IssueSummary{Number: 42, Title: "Cached issue"},
			Body:         "Issue body.",
			Comments:     []gh.Comment{{Body: "A comment."}},
		},
	}

	first := model.detailLines()
	second := model.detailLines()
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("detail lines were rendered more than once for the same view")
	}

	model.width = 60
	resized := model.detailLines()
	if &first[0] == &resized[0] {
		t.Fatal("detail lines cache was not rebuilt after a width change")
	}

	model.commentsExpanded = true
	expanded := model.detailLines()
	if &resized[0] == &expanded[0] {
		t.Fatal("detail lines cache was not rebuilt after expanding comments")
	}
}

func TestDetailMouseWheelScrollsByViewportStep(t *testing.T) {
	model := &Model{
		screen: screenDetail,
		width:  80,
		height: 12,
		detail: &gh.IssueDetail{
			IssueSummary: gh.IssueSummary{Number: 42, Title: "Scrollable issue"},
			Body:         strings.Repeat("A long line of issue content.\n", 40),
		},
	}

	model.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if model.detailOffset != 3 {
		t.Fatalf("wheel down offset = %d, want 3", model.detailOffset)
	}
	model.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if model.detailOffset != 0 {
		t.Fatalf("wheel up offset = %d, want 0", model.detailOffset)
	}
}

func TestIssueFilterMatchesIssueNumber(t *testing.T) {
	model := &Model{
		groups: buildGroups(gh.Summary{Issues: []gh.IssueSummary{
			{Number: 42, Title: "A different title"},
			{Number: 7, Title: "Another issue"},
		}}),
		filter: "42",
	}

	issues := model.filteredIssues()
	if len(issues) != 1 || issues[0].Number != 42 {
		t.Fatalf("filtered issues = %#v, want issue #42", issues)
	}
}

func TestIssueLineMarksBlockedIssues(t *testing.T) {
	line := issueLine(gh.IssueSummary{Number: 234, Title: "Blocked work", BlockedByCount: 1}, false)
	if !strings.Contains(line, "(b) #234 Blocked work") {
		t.Fatalf("issue line = %q, want blocked marker", line)
	}
}

func TestPreviewShowsDependencyNames(t *testing.T) {
	repository, _ := gh.ParseRepositoryRef("octo/demo")
	model := &Model{
		repo: repository,
		groups: buildGroups(gh.Summary{Issues: []gh.IssueSummary{{
			Number:         42,
			Title:          "Blocked work",
			State:          "open",
			BlockedByCount: 1,
			BlockingCount:  1,
		}}}),
		dependencyCache: map[int]gh.IssueDependencies{
			42: {
				BlockedBy: []gh.IssueReference{{Number: 234, Title: "Prerequisite"}},
				Blocking:  []gh.IssueReference{{Number: 235, Title: "Follow-up"}},
			},
		},
	}

	preview := strings.Join(model.previewLines(20), "\n")
	for _, expected := range []string{
		"Blocked by (1):",
		"#234 Prerequisite",
		"Blocking (1):",
		"#235 Follow-up",
	} {
		if !strings.Contains(preview, expected) {
			t.Errorf("preview does not contain %q:\n%s", expected, preview)
		}
	}
}
