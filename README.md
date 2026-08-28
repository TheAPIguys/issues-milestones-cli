# i-gh

`i-gh` is a keyboard-driven Bubble Tea terminal UI for browsing open GitHub
issues by active milestone. It uses the GitHub CLI (`gh`) for authentication
and API access, so no separate GitHub token setup is required. It optionally
stores the last selected repository in the application configuration.

The dashboard shows milestones, issue counts, issue labels, comments, and
dependency indicators in three panes. The selected issue preview can show the
issues that block it and the issues it is blocking. A detail view provides the
complete Markdown-rendered issue, comments, dependency relationships, browser
opening, clipboard export, and local worktree creation.

## Features

- Browse open issues grouped by active milestone.
- Keep unassigned issues in a dedicated `No milestone` group.
- Keep issues assigned to unavailable milestones in `Other milestones`.
- Sort issues from the smallest issue number to the largest.
- Filter issues and repositories locally.
- Read issue bodies, metadata, comments, and dependencies.
- Copy a complete issue as Markdown.
- Open an issue in the browser through `gh`.
- Create a local worktree with `git-wt switch --create i-<issue number>`.
- Support GitHub Enterprise references in `HOST/OWNER/REPO` form.
- Remember the last repository selected by the user.

## Requirements

- Go 1.25 or newer.
- GitHub CLI (`gh`) installed and available on `PATH`.
- A GitHub CLI account authenticated with access to the repositories you want
  to browse.
- `git-wt` installed and available on `PATH` if you want to use the `w`
  worktree action.

Check the required tools with:

```text
go version
gh --version
git-wt --version
```

`git-wt` is optional. The rest of the application works without it.

## Installation

### Install the v0.2.0 release

No clone is required. Install the published release directly with Go:

```text
go install github.com/TheAPIguys/issues-milestones-cli/cmd/i-gh@v0.2.0
```

Use a newer release tag in this command when one is published.

### Install from source

Clone this repository and run the following command from its root directory:

```text
go install ./cmd/i-gh
```

The `i-gh` executable is installed in the Go binary directory. If the command
is not found afterwards, add the directory printed by this command to your
`PATH`:

```text
go env GOPATH
```

On Windows, the executable is normally placed in `%USERPROFILE%\go\bin`.
On Linux and macOS, it is normally placed in `$(go env GOPATH)/bin`.

### Build a local binary

From the repository root:

```text
go build -o i-gh ./cmd/i-gh
```

On Windows, build an executable with:

```text
go build -o i-gh.exe ./cmd/i-gh
```

## Authentication

`i-gh` uses the existing GitHub CLI authentication. Authenticate once before
launching the application:

```text
gh auth login
gh auth status
```

For private repositories, the authenticated GitHub account must have access to
the repository. `i-gh` does not store or manage GitHub credentials.

## Usage

Launch the installed command without a repository argument:

```text
i-gh
```

When no `--repo` argument is supplied, `i-gh` tries these sources in order:

1. The GitHub repository for the current checkout.
2. The last repository selected in `i-gh`.
3. A repository picker populated from `gh repo list`.

Open a specific repository with its owner and name:

```text
i-gh --repo OWNER/REPO
```

For example:

```text
i-gh --repo octocat/Hello-World
i-gh --repo TheAPIGuys/rfe-new-portal-app-sveltekit
```

GitHub Enterprise repositories can include the host:

```text
i-gh --repo github.example.com/team/project
```

During development, run the application directly from the source tree:

```text
go run ./cmd/i-gh --repo OWNER/REPO
```

`OWNER/REPO` is a placeholder. For a repository URL such as
`https://github.com/alice/my-project`, use `alice/my-project`.

## Typical Workflow

1. Launch `i-gh` from a terminal.
2. Select a repository if one was not resolved automatically.
3. Use the milestone pane to choose a milestone or `All open`.
4. Move to the issue pane and select an issue.
5. Press `Enter` to open the full issue detail view.
6. Press `c` to expand or collapse comments.
7. Press `y` to copy the issue as Markdown.
8. Press `o` to open the issue in a browser.
9. Press `w` to create the issue worktree.

For issue `#266`, the worktree action runs:

```text
git-wt switch --create i-266
```

The worktree command runs from the current local Git checkout. It creates the
worktree and branch, but the parent shell's current directory cannot be
changed by a child process. The command result is reported in the UI.

## Controls

| Key | Action |
| --- | --- |
| `j` / `down` | Move down or scroll down |
| `k` / `up` | Move up or scroll up |
| `h` / `left` | Focus the milestone pane |
| `l` / `right` / `Tab` | Focus the issue pane |
| `Enter` | Select a milestone or open an issue |
| `/` | Filter the current list |
| `c` | Expand or collapse comments in the detail view |
| `y` | Copy the complete issue as Markdown |
| `o` | Open the issue in a browser |
| `w` | Create the `i-<issue number>` `git-wt` worktree |
| `r` | Refresh the current repository or issue |
| `R` | Choose another repository |
| `Esc` | Return from the detail view or picker |
| `q` / `Ctrl+C` | Quit |

## Screenshots

The project screenshots show these two views:

- Dashboard: milestones, sorted issues, dependency markers, and the selected
  issue preview.
- Issue detail: rendered issue Markdown, metadata, dependency lists, and
  comments.

Screenshot files are not currently included in this checkout. When they are
available, place them at these paths to display them here:

```text
docs/screenshots/dashboard.png
docs/screenshots/issue-detail.png
```

## Troubleshooting

### `gh` is not found

Install the GitHub CLI and make sure `gh` is available on `PATH`:

```text
gh --version
```

### Authentication or repository access fails

Run:

```text
gh auth status
gh repo view OWNER/REPO
```

The authenticated account must be able to read the repository.

### Worktree creation fails

Confirm that `git-wt` is installed:

```text
git-wt --version
```

The `w` action must be run from a local Git checkout because `git-wt` operates
on the current working tree. A repository selected from the picker is a GitHub
repository reference; it does not clone that repository automatically.

## Scope

`i-gh` does not create, edit, close, or comment on GitHub issues. It does not
edit milestones or dependencies. The `w` action is the only local repository
mutation and creates a Git worktree through `git-wt`.

## Development Checks

Run the project checks from the repository root:

```text
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/i-gh
```
