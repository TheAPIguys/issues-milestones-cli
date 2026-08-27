package format

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"

	"github.com/TheAPIguys/issues-milestones-cli/internal/gh"
)

// IssueMarkdown creates the text used by both the detail view and clipboard.
func IssueMarkdown(repository gh.Repository, issue gh.IssueDetail, includeComments bool) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "# #%d %s\n\n", issue.Number, issue.Title)
	fmt.Fprintf(&builder, "- Repository: `%s`\n", repository.String())
	fmt.Fprintf(&builder, "- State: `%s`\n", issue.State)
	fmt.Fprintf(&builder, "- URL: %s\n", issue.URL)
	if issue.Author != "" {
		fmt.Fprintf(&builder, "- Author: @%s\n", issue.Author)
	}
	if issue.Milestone != nil {
		fmt.Fprintf(&builder, "- Milestone: %s\n", issue.Milestone.Title)
	}
	if len(issue.Labels) > 0 {
		labels := make([]string, 0, len(issue.Labels))
		for _, label := range issue.Labels {
			labels = append(labels, label.Name)
		}
		fmt.Fprintf(&builder, "- Labels: %s\n", strings.Join(labels, ", "))
	}
	fmt.Fprintf(&builder, "- Comments: %d\n", issue.CommentCount)

	builder.WriteString("\n")
	if strings.TrimSpace(issue.Body) == "" {
		builder.WriteString("_No description provided._\n")
	} else {
		builder.WriteString(strings.TrimSpace(issue.Body))
		builder.WriteString("\n")
	}

	if len(issue.BlockedBy) > 0 || len(issue.Blocking) > 0 {
		builder.WriteString("\n## Dependencies\n")
		for _, dependency := range issue.BlockedBy {
			fmt.Fprintf(&builder, "- Blocked by #%d %s (%s)\n", dependency.Number, dependency.Title, dependency.URL)
		}
		for _, dependency := range issue.Blocking {
			fmt.Fprintf(&builder, "- Blocks #%d %s (%s)\n", dependency.Number, dependency.Title, dependency.URL)
		}
	}

	if includeComments && len(issue.Comments) > 0 {
		builder.WriteString("\n## Comments\n")
		for _, comment := range issue.Comments {
			author := comment.Author
			if author == "" {
				author = "unknown"
			}
			fmt.Fprintf(&builder, "\n### @%s\n\n", author)
			builder.WriteString(strings.TrimSpace(comment.Body))
			builder.WriteString("\n")
		}
	}

	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func RenderMarkdown(markdown string, width int) (string, error) {
	if width < 20 {
		width = 20
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(markdown)
}
