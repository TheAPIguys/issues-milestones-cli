# i-gh Plan

## Goal

`i-gh` is a read-focused terminal UI for browsing open GitHub issues grouped by
active milestones. It uses the user's existing `gh` authentication and does
not manage GitHub tokens itself.

The first command should be:

```text
i-gh
```

An explicit repository should also be supported:

```text
i-gh --repo OWNER/REPO
```

## MVP Scope

- Resolve a repository from an explicit flag, the current GitHub checkout, or
  an interactive repository picker.
- Remember the last selected repository in the user's application config.
- Load all open issues and all open milestones, with pagination.
- Group issues under their milestone and provide a `No milestone` group.
- Keep open issues assigned to a closed or otherwise unavailable milestone in
  an `Other milestones` group so no issue disappears.
- Show issue number, title, labels, comment count, and dependency indicators.
- Open an issue detail view showing its Markdown body, metadata, comments, and
  `blocked by` / `blocking` relationships.
- Expand and collapse comments without refetching an already loaded issue.
- Copy a complete Markdown representation of the selected issue to the system
  clipboard.
- Refresh data and display actionable errors when `gh` is missing,
  unauthenticated, rate-limited, or the repository cannot be accessed.

Closed issues and closed milestones are excluded by default. They can be added
later as an explicit filter rather than making the initial screen noisy.

## Repository Resolution

Use this order when `i-gh` starts:

1. `--repo HOST/OWNER/REPO` when supplied.
2. `gh repo view --json nameWithOwner` in the current directory.
3. The last repository saved by `i-gh`.
4. A picker populated by `gh repo list --json ...`.

The picker should allow text filtering and selection with `Enter`. Selecting a
repository saves it as the next default. `Esc` cancels the picker and `q`
exits.

This makes an empty directory usable while still making `i-gh` convenient when
run from a checkout.

## Main Screen

On a sufficiently wide terminal, use three panes:

```text
+------------------+------------------------------+--------------------------+
| Milestones       | Issues                       | Selected issue           |
|                  |                              |                          |
| All open         | #123 Fix login flow          | #123 Fix login flow      |
| v1.0             | #119 Update docs       [2]  | state: open              |
| v1.1             | #107 Add retries  [blocked]  | milestone: v1.0          |
| No milestone     |                              | body and relationships   |
| Other milestones |                              | comments (collapsible)   |
+------------------+------------------------------+--------------------------+
```

The exact styling can be decided during implementation, but the selected
milestone and issue must always be visually distinct. Counts should be shown
beside each milestone. The `No milestone` entry is a first-class group, not a
special case hidden at the bottom of the issue list.

For narrow terminals, switch to a focused view instead of squeezing three
panes horizontally:

- milestone list view
- issue list view
- issue detail view

`Tab` or the left/right arrows move between panes on wide screens. `Enter`
opens the selected issue in the detail view, and `Esc` returns to the previous
view on narrow screens.

## Suggested Key Bindings

| Key | Action |
| --- | --- |
| `j` / `down` | Move down |
| `k` / `up` | Move up |
| `h` / `left` | Focus the previous pane |
| `l` / `right` | Focus the next pane |
| `Enter` | Select milestone or open issue detail |
| `Esc` | Close detail, filter, or picker |
| `/` | Filter milestones/issues |
| `c` | Expand or collapse issue comments in detail |
| `y` | Copy the selected issue as Markdown |
| `o` | Open the issue in the browser through `gh` |
| `w` | Create a local `git-wt` worktree named `i-NUMBER` |
| `r` | Refresh the current repository |
| `R` | Choose a different repository |
| `q` / `Ctrl+C` | Quit |

The footer should show context-sensitive bindings and a short status message,
such as `Copied issue #123 to clipboard`.

## GitHub CLI Integration

All external GitHub access should go through an injectable `gh` command
runner. The application must pass argument slices to `exec.CommandContext`,
never build shell command strings, and inherit the user's `gh` environment.

Use the high-level commands where they provide the required fields:

- Repository detection: `gh repo view`, `gh repo list`
- Issue detail/browser: `gh issue view NUMBER --repo REPO --json ...` and
  `gh issue view NUMBER --repo REPO --web`

Use `gh api` for paginated collections where complete results matter:

- `repos/OWNER/REPO/milestones?state=open&per_page=100`
- `repos/OWNER/REPO/issues?state=open&per_page=100`
- `repos/OWNER/REPO/issues/NUMBER/comments?per_page=100`
- `repos/OWNER/REPO/issues/NUMBER/dependencies/blocked_by`
- `repos/OWNER/REPO/issues/NUMBER/dependencies/blocking`

The issue endpoint also returns pull requests. Filter entries with a
`pull_request` object so the UI remains issue-only. `gh api --paginate
--slurp` can provide valid JSON for the Go decoder; the adapter should flatten
the page arrays and handle an empty result cleanly.

The summary response should not load full comments for every issue. Load the
selected issue's body, comments, and dependency references lazily and cache
the result by repository and issue number.

## Domain Data

The UI should consume small application types rather than raw GitHub JSON:

- `Repository`: host, owner, name, display name, URL, private flag.
- `Milestone`: number, title, description, state, due date, open/closed
  counts, URL.
- `IssueSummary`: number, title, state, labels, milestone reference,
  comment count, dependency counts, URL, updated time.
- `IssueDetail`: summary data, author, body, assignees, timestamps, comments,
  blocked-by references, and blocking references.
- `Comment`: author, body, created time, updated time.
- `IssueReference`: number, title, state, URL.

Grouping should be a pure function so it can be tested independently of the
Bubble Tea model. Preserve the API order for issues or sort by updated time;
choose one consistent rule and document it in the UI.

## Proposed Project Shape

Keep the first implementation small and separable:

```text
cmd/i-gh/main.go          CLI flags, startup, Bubble Tea program
internal/app/             Bubble Tea model, update loop, views, key map
internal/gh/              gh runner, JSON decoding, repository data access
internal/config/          last-repository persistence
internal/format/          Markdown rendering for detail and clipboard output
```

Recommended dependencies:

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles` for viewport, list, and text input
- `github.com/charmbracelet/lipgloss`
- `github.com/charmbracelet/glamour` for readable Markdown rendering
- `github.com/atotto/clipboard` for cross-platform clipboard support

The `gh` adapter should expose an interface that can be replaced by a fake in
tests. Do not make Bubble Tea tests execute the real GitHub CLI.

## Implementation Phases

### Phase 1: Bootstrap

- Initialize the Go module and executable named `i-gh`.
- Add the Bubble Tea shell and a basic `gh` runner.
- Add `--repo`, `--help`, and a clear missing-authentication error.

### Phase 2: Repository and Summary Data

- Implement repository resolution and config persistence.
- Implement paginated milestone and open-issue loading.
- Add loading, empty, retry, and error states.
- Implement tested milestone/no-milestone grouping.

### Phase 3: Browsing UI

- Build the milestone and issue panes.
- Add keyboard navigation, filtering, counts, responsive layout, and refresh.
- Preserve selections when refreshing where the selected number still exists.

### Phase 4: Issue Details

- Add lazy detail loading and a scrollable Markdown body.
- Add dependency sections for `blocked by` and `blocking`.
- Add comment expansion/collapse and cached detail responses.
- Add browser launch and clipboard copy.

### Phase 5: Reliability and Release Polish

- Test command construction, JSON fixtures, grouping, formatting, config, and
  key handling.
- Verify Windows terminal behavior and clipboard support, then check Linux and
  macOS assumptions.
- Add README setup instructions, screenshots, release build commands, and a
  note that `gh auth login` is required.

## Test and Verification Strategy

- Unit tests for JSON decoding, pagination flattening, issue grouping,
  repository resolution, Markdown clipboard formatting, and error mapping.
- Bubble Tea update tests using synthetic `WindowSizeMsg`, loading messages,
  key messages, and detail messages.
- Fake `gh` runner tests to verify no shell interpolation and correct arguments.
- Manual smoke test against a repository containing multiple milestones, an
  unassigned issue, comments, and dependency relationships.

Expected checks once code exists:

```text
go test ./...
go vet ./...
go build ./cmd/i-gh
go run ./cmd/i-gh --repo OWNER/REPO
```

## Deliberate Non-Goals for the First Release

- Creating, editing, closing, or commenting on issues.
- Editing milestones or dependencies.
- Pull request browsing.
- GitHub Projects, notifications, or issue search syntax beyond local filter.
- A separate token/authentication system.

These can be added after the read-only browsing loop is stable.
