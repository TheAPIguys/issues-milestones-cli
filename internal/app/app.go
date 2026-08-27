package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/TheAPIguys/issues-milestones-cli/internal/config"
	"github.com/TheAPIguys/issues-milestones-cli/internal/format"
	"github.com/TheAPIguys/issues-milestones-cli/internal/gh"
)

const wideLayoutWidth = 100

type screen int

const (
	screenResolving screen = iota
	screenPicker
	screenLoading
	screenDashboard
	screenDetail
	screenError
)

type pane int

const (
	paneMilestones pane = iota
	paneIssues
)

type issueGroup struct {
	ID        string
	Title     string
	Milestone *gh.Milestone
	Issues    []gh.IssueSummary
}

type resolveMsg struct {
	repository   gh.Repository
	repositories []gh.Repository
	err          error
}

type repositoriesMsg struct {
	repositories []gh.Repository
	err          error
}

type summaryMsg struct {
	repository gh.Repository
	summary    gh.Summary
	err        error
}

type detailMsg struct {
	repository gh.Repository
	number     int
	detail     gh.IssueDetail
	err        error
}

type dependenciesMsg struct {
	repository   gh.Repository
	number       int
	dependencies gh.IssueDependencies
	err          error
}

type copyMsg struct {
	err error
}

type browserMsg struct {
	err error
}

type worktreeMsg struct {
	repository gh.Repository
	number     int
	err        error
}

type spinnerMsg struct{}

type Model struct {
	client       *gh.Client
	config       config.Config
	explicitRepo string

	screen screen
	width  int
	height int
	spin   int

	repo            gh.Repository
	repositories    []gh.Repository
	repoCursor      int
	pickerFilter    string
	pickerFiltering bool
	pickerLoading   bool

	summary     *gh.Summary
	groups      []issueGroup
	groupCursor int
	issueCursor int
	activePane  pane
	filter      string
	filtering   bool

	detail           *gh.IssueDetail
	detailCache      map[int]gh.IssueDetail
	detailNumber     int
	detailLoading    bool
	detailError      string
	commentsExpanded bool
	detailOffset     int
	worktreeLoading  bool

	dependencyCache   map[int]gh.IssueDependencies
	dependencyLoading bool
	dependencyNumber  int
	dependencyError   string

	status       string
	errorMessage string
}

func New(client *gh.Client, storedConfig config.Config, explicitRepo string) *Model {
	return &Model{
		client:          client,
		config:          storedConfig,
		explicitRepo:    explicitRepo,
		screen:          screenResolving,
		activePane:      paneMilestones,
		detailCache:     make(map[int]gh.IssueDetail),
		dependencyCache: make(map[int]gh.IssueDependencies),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.resolveCmd(), m.spinnerCmd())
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.clampDetailOffset()
		return m, nil
	case spinnerMsg:
		m.spin = (m.spin + 1) % len(spinnerFrames)
		return m, m.spinnerCmd()
	case resolveMsg:
		if message.err != nil {
			m.errorMessage = gh.FriendlyError(message.err)
			m.screen = screenError
			return m, nil
		}
		if message.repository.Ref == "" {
			m.repositories = message.repositories
			m.repoCursor = 0
			m.pickerFilter = ""
			m.pickerFiltering = false
			m.pickerLoading = false
			m.screen = screenPicker
			return m, nil
		}
		m.selectRepository(message.repository)
		m.screen = screenLoading
		return m, m.loadSummaryCmd(message.repository)
	case repositoriesMsg:
		m.pickerLoading = false
		if message.err != nil {
			m.errorMessage = gh.FriendlyError(message.err)
			m.screen = screenError
			return m, nil
		}
		m.repositories = message.repositories
		m.repoCursor = 0
		m.pickerFilter = ""
		m.pickerFiltering = false
		m.screen = screenPicker
		return m, nil
	case summaryMsg:
		if message.repository.Ref != m.repo.Ref {
			return m, nil
		}
		if message.err != nil {
			m.errorMessage = gh.FriendlyError(message.err)
			m.screen = screenError
			return m, nil
		}
		oldGroupID := m.currentGroupID()
		oldIssueNumber := m.selectedIssueNumber()
		summary := message.summary
		m.summary = &summary
		m.groups = buildGroups(summary)
		m.restoreSelection(oldGroupID, oldIssueNumber)
		m.detail = nil
		m.detailError = ""
		m.detailLoading = false
		m.detailCache = make(map[int]gh.IssueDetail)
		m.dependencyCache = make(map[int]gh.IssueDependencies)
		m.dependencyLoading = false
		m.dependencyError = ""
		m.screen = screenDashboard
		m.errorMessage = ""
		m.status = fmt.Sprintf("Updated %s at %s", m.repo.String(), time.Now().Format("15:04:05"))
		return m, m.loadSelectedDependenciesCmd()
	case detailMsg:
		if message.repository.Ref != m.repo.Ref || message.number != m.detailNumber {
			return m, nil
		}
		m.detailLoading = false
		if message.err != nil {
			m.detail = nil
			m.detailError = gh.FriendlyError(message.err)
			return m, nil
		}
		detail := message.detail
		m.detailCache[message.number] = detail
		m.detail = &detail
		m.detailError = ""
		m.detailOffset = 0
		m.commentsExpanded = false
		m.dependencyCache[message.number] = gh.IssueDependencies{
			BlockedBy: message.detail.BlockedBy,
			Blocking:  message.detail.Blocking,
		}
		return m, nil
	case dependenciesMsg:
		if message.repository.Ref != m.repo.Ref || message.number != m.dependencyNumber {
			return m, nil
		}
		m.dependencyLoading = false
		if message.err != nil {
			m.dependencyError = gh.FriendlyError(message.err)
			return m, nil
		}
		m.dependencyCache[message.number] = message.dependencies
		m.dependencyError = ""
		return m, nil
	case copyMsg:
		if message.err != nil {
			m.status = "Copy failed: " + message.err.Error()
		} else {
			m.status = fmt.Sprintf("Copied issue #%d to clipboard", m.detailNumber)
		}
		return m, nil
	case browserMsg:
		if message.err != nil {
			m.status = "Browser failed: " + gh.FriendlyError(message.err)
		} else {
			m.status = fmt.Sprintf("Opened issue #%d in the browser", m.detailNumber)
		}
		return m, nil
	case worktreeMsg:
		if message.repository.Ref != m.repo.Ref || message.number != m.detailNumber {
			return m, nil
		}
		m.worktreeLoading = false
		if message.err != nil {
			m.status = "Worktree failed: " + gh.FriendlyError(message.err)
		} else {
			m.status = fmt.Sprintf("Created worktree i-%d", message.number)
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) resolveCmd() tea.Cmd {
	client := m.client
	explicit := m.explicitRepo
	last := m.config.LastRepository
	return func() tea.Msg {
		repository, repositories, err := client.ResolveRepository(context.Background(), explicit, last)
		return resolveMsg{repository: repository, repositories: repositories, err: err}
	}
}

func (m *Model) listRepositoriesCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		repositories, err := client.ListRepositories(context.Background())
		return repositoriesMsg{repositories: repositories, err: err}
	}
}

func (m *Model) loadSummaryCmd(repository gh.Repository) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		summary, err := client.LoadSummary(context.Background(), repository)
		return summaryMsg{repository: repository, summary: summary, err: err}
	}
}

func (m *Model) loadDetailCmd(repository gh.Repository, number int) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		detail, err := client.LoadIssue(context.Background(), repository, number)
		return detailMsg{repository: repository, number: number, detail: detail, err: err}
	}
}

func (m *Model) loadDependenciesCmd(repository gh.Repository, number int) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		dependencies, err := client.LoadIssueDependencies(context.Background(), repository, number)
		return dependenciesMsg{
			repository:   repository,
			number:       number,
			dependencies: dependencies,
			err:          err,
		}
	}
}

func (m *Model) loadSelectedDependenciesCmd() tea.Cmd {
	issue, ok := m.selectedIssue()
	if !ok || issue.BlockedByCount == 0 && issue.BlockingCount == 0 {
		return nil
	}
	if _, ok := m.dependencyCache[issue.Number]; ok {
		return nil
	}
	if m.dependencyLoading && m.dependencyNumber == issue.Number {
		return nil
	}
	m.dependencyLoading = true
	m.dependencyNumber = issue.Number
	m.dependencyError = ""
	return m.loadDependenciesCmd(m.repo, issue.Number)
}

func (m *Model) copyCmd(repository gh.Repository, detail gh.IssueDetail) tea.Cmd {
	text := format.IssueMarkdown(repository, detail, true)
	return func() tea.Msg {
		return copyMsg{err: clipboard.WriteAll(text)}
	}
}

func (m *Model) openBrowserCmd(repository gh.Repository, number int) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		return browserMsg{err: client.OpenIssue(context.Background(), repository, number)}
	}
}

func (m *Model) createWorktreeCmd(repository gh.Repository, number int) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		return worktreeMsg{
			repository: repository,
			number:     number,
			err:        client.CreateWorktree(context.Background(), number),
		}
	}
}

func (m *Model) spinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerMsg{}
	})
}

func (m *Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if message.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.screen {
	case screenResolving, screenLoading:
		if message.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	case screenPicker:
		return m.handlePickerKey(message)
	case screenDashboard:
		return m.handleDashboardKey(message)
	case screenDetail:
		return m.handleDetailKey(message)
	case screenError:
		return m.handleErrorKey(message)
	default:
		return m, nil
	}
}

func (m *Model) handlePickerKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickerFiltering {
		m.handleFilterInput(message, true)
		return m, nil
	}

	switch message.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.repo.Ref == "" {
			m.errorMessage = "Select a repository or press q to quit"
			m.screen = screenError
		} else {
			m.screen = screenDashboard
		}
	case "/":
		m.pickerFiltering = true
		m.status = "Type to filter repositories, Enter to apply, Esc to cancel"
	case "up", "k":
		m.moveRepository(-1)
	case "down", "j":
		m.moveRepository(1)
	case "enter":
		repositories := m.filteredRepositories()
		if len(repositories) > 0 {
			m.selectRepository(repositories[m.repoCursor])
			m.screen = screenLoading
			return m, m.loadSummaryCmd(m.repo)
		}
	case "r":
		m.pickerLoading = true
		return m, m.listRepositoriesCmd()
	}
	return m, nil
}

func (m *Model) handleDashboardKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		m.handleFilterInput(message, false)
		return m, nil
	}

	switch message.String() {
	case "q":
		return m, tea.Quit
	case "/":
		m.filtering = true
		m.status = "Type to filter issues, Enter to apply, Esc to cancel"
	case "r":
		m.screen = screenLoading
		m.detail = nil
		m.status = "Refreshing..."
		return m, m.loadSummaryCmd(m.repo)
	case "R":
		return m.startPicker()
	case "up", "k":
		if m.activePane == paneMilestones {
			m.moveGroup(-1)
		} else {
			m.moveIssue(-1)
		}
		return m, m.loadSelectedDependenciesCmd()
	case "down", "j":
		if m.activePane == paneMilestones {
			m.moveGroup(1)
		} else {
			m.moveIssue(1)
		}
		return m, m.loadSelectedDependenciesCmd()
	case "left", "h", "shift+tab":
		m.activePane = paneMilestones
	case "right", "l", "tab":
		m.activePane = paneIssues
		return m, m.loadSelectedDependenciesCmd()
	case "enter":
		if m.activePane == paneMilestones {
			m.activePane = paneIssues
			return m, m.loadSelectedDependenciesCmd()
		} else {
			return m.openDetail()
		}
	case "o":
		if issue, ok := m.selectedIssue(); ok {
			m.detailNumber = issue.Number
			m.status = "Opening issue in browser..."
			return m, m.openBrowserCmd(m.repo, issue.Number)
		}
	}
	return m, nil
}

func (m *Model) handleDetailKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detailLoading {
		if message.String() == "esc" {
			m.screen = screenDashboard
			m.detailLoading = false
		}
		if message.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}

	switch message.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.screen = screenDashboard
	case "c":
		if m.detail != nil && len(m.detail.Comments) > 0 {
			m.commentsExpanded = !m.commentsExpanded
			m.detailOffset = 0
		}
	case "y":
		if m.detail != nil {
			return m, m.copyCmd(m.repo, *m.detail)
		}
	case "o":
		if m.detail != nil {
			m.status = "Opening issue in browser..."
			return m, m.openBrowserCmd(m.repo, m.detail.Number)
		}
	case "w":
		if m.detail != nil {
			if m.worktreeLoading {
				m.status = "Worktree creation is already in progress"
				return m, nil
			}
			m.worktreeLoading = true
			m.status = fmt.Sprintf("Creating worktree i-%d...", m.detail.Number)
			return m, m.createWorktreeCmd(m.repo, m.detail.Number)
		}
	case "r":
		if m.detail != nil {
			m.detailLoading = true
			m.detailError = ""
			m.worktreeLoading = false
			m.dependencyLoading = false
			m.dependencyError = ""
			delete(m.detailCache, m.detail.Number)
			return m, m.loadDetailCmd(m.repo, m.detail.Number)
		}
	case "R":
		return m.startPicker()
	case "up", "k":
		m.detailOffset--
	case "down", "j":
		m.detailOffset++
	case "pgup":
		m.detailOffset -= max(1, m.height/2)
	case "pgdown":
		m.detailOffset += max(1, m.height/2)
	case "home":
		m.detailOffset = 0
	case "end":
		m.detailOffset = m.maxDetailOffset()
	}
	m.clampDetailOffset()
	return m, nil
}

func (m *Model) handleErrorKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "q":
		return m, tea.Quit
	case "r":
		if m.repo.Ref == "" {
			m.screen = screenResolving
			m.errorMessage = ""
			return m, m.resolveCmd()
		}
		m.screen = screenLoading
		m.errorMessage = ""
		return m, m.loadSummaryCmd(m.repo)
	case "R":
		return m.startPicker()
	case "esc":
		if m.repo.Ref != "" && m.summary != nil {
			m.screen = screenDashboard
		}
	}
	return m, nil
}

func (m *Model) handleFilterInput(message tea.KeyMsg, picker bool) {
	switch message.Type {
	case tea.KeyEsc:
		if picker {
			m.pickerFiltering = false
		} else {
			m.filtering = false
		}
	case tea.KeyEnter:
		if picker {
			m.pickerFiltering = false
		} else {
			m.filtering = false
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if picker {
			m.pickerFilter = trimLastRune(m.pickerFilter)
			m.repoCursor = 0
		} else {
			m.filter = trimLastRune(m.filter)
			m.issueCursor = 0
		}
	case tea.KeyCtrlU:
		if picker {
			m.pickerFilter = ""
			m.repoCursor = 0
		} else {
			m.filter = ""
			m.issueCursor = 0
		}
	case tea.KeyRunes:
		if picker {
			m.pickerFilter += string(message.Runes)
			m.repoCursor = 0
		} else {
			m.filter += string(message.Runes)
			m.issueCursor = 0
		}
	}
}

func (m *Model) startPicker() (tea.Model, tea.Cmd) {
	m.screen = screenPicker
	m.pickerLoading = true
	m.pickerFilter = ""
	m.pickerFiltering = false
	m.repoCursor = 0
	m.errorMessage = ""
	return m, m.listRepositoriesCmd()
}

func (m *Model) selectRepository(repository gh.Repository) {
	m.repo = repository
	if err := m.config.SetLastRepository(repository.Ref); err != nil {
		m.status = "Repository selected, but could not save default: " + err.Error()
	} else {
		m.status = "Selected " + repository.String()
	}
	m.summary = nil
	m.groups = nil
	m.detail = nil
	m.detailCache = make(map[int]gh.IssueDetail)
	m.dependencyCache = make(map[int]gh.IssueDependencies)
	m.detailError = ""
	m.detailLoading = false
	m.dependencyLoading = false
	m.dependencyError = ""
	m.detailOffset = 0
	m.groupCursor = 0
	m.issueCursor = 0
}

func (m *Model) openDetail() (tea.Model, tea.Cmd) {
	issue, ok := m.selectedIssue()
	if !ok {
		m.status = "There are no issues in this group"
		return m, nil
	}
	m.detailNumber = issue.Number
	m.detailError = ""
	m.commentsExpanded = false
	m.detailOffset = 0
	if detail, ok := m.detailCache[issue.Number]; ok {
		m.detail = &detail
		m.screen = screenDetail
		m.detailLoading = false
		return m, nil
	}
	m.detail = nil
	m.detailLoading = true
	m.screen = screenDetail
	return m, m.loadDetailCmd(m.repo, issue.Number)
}

func (m *Model) moveRepository(delta int) {
	repositories := m.filteredRepositories()
	if len(repositories) == 0 {
		m.repoCursor = 0
		return
	}
	m.repoCursor = clamp(m.repoCursor+delta, 0, len(repositories)-1)
}

func (m *Model) moveGroup(delta int) {
	if len(m.groups) == 0 {
		m.groupCursor = 0
		return
	}
	m.groupCursor = clamp(m.groupCursor+delta, 0, len(m.groups)-1)
	m.issueCursor = 0
	m.detail = nil
	m.detailError = ""
}

func (m *Model) moveIssue(delta int) {
	issues := m.filteredIssues()
	if len(issues) == 0 {
		m.issueCursor = 0
		return
	}
	m.issueCursor = clamp(m.issueCursor+delta, 0, len(issues)-1)
	m.detail = nil
	m.detailError = ""
}

func (m *Model) filteredRepositories() []gh.Repository {
	if m.pickerFilter == "" {
		return m.repositories
	}
	needle := strings.ToLower(m.pickerFilter)
	filtered := make([]gh.Repository, 0, len(m.repositories))
	for _, repository := range m.repositories {
		if strings.Contains(strings.ToLower(repository.String()+" "+repository.Description), needle) {
			filtered = append(filtered, repository)
		}
	}
	return filtered
}

func (m *Model) filteredIssues() []gh.IssueSummary {
	if len(m.groups) == 0 || m.groupCursor >= len(m.groups) {
		return nil
	}
	issues := m.groups[m.groupCursor].Issues
	if m.filter == "" {
		return issues
	}
	needle := strings.ToLower(m.filter)
	filtered := make([]gh.IssueSummary, 0, len(issues))
	for _, issue := range issues {
		parts := []string{strconv.Itoa(issue.Number), issue.Title, issue.Author, issue.URL}
		for _, label := range issue.Labels {
			parts = append(parts, label.Name)
		}
		if issue.Milestone != nil {
			parts = append(parts, issue.Milestone.Title)
		}
		if strings.Contains(strings.ToLower(strings.Join(parts, " ")), needle) {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

func (m *Model) selectedIssue() (gh.IssueSummary, bool) {
	issues := m.filteredIssues()
	if len(issues) == 0 || m.issueCursor >= len(issues) {
		return gh.IssueSummary{}, false
	}
	return issues[m.issueCursor], true
}

func (m *Model) currentGroupID() string {
	if m.groupCursor >= 0 && m.groupCursor < len(m.groups) {
		return m.groups[m.groupCursor].ID
	}
	return ""
}

func (m *Model) selectedIssueNumber() int {
	issue, ok := m.selectedIssue()
	if !ok {
		return 0
	}
	return issue.Number
}

func (m *Model) restoreSelection(groupID string, issueNumber int) {
	m.groupCursor = 0
	for index, group := range m.groups {
		if group.ID == groupID {
			m.groupCursor = index
			break
		}
	}
	m.issueCursor = 0
	if issueNumber == 0 {
		return
	}
	for index, issue := range m.filteredIssues() {
		if issue.Number == issueNumber {
			m.issueCursor = index
			break
		}
	}
}

func buildGroups(summary gh.Summary) []issueGroup {
	groups := []issueGroup{{
		ID:     "all",
		Title:  "All open",
		Issues: append([]gh.IssueSummary(nil), summary.Issues...),
	}}

	milestones := append([]gh.Milestone(nil), summary.Milestones...)
	sort.SliceStable(milestones, func(i, j int) bool {
		return milestones[i].Number < milestones[j].Number
	})

	groupByMilestone := make(map[int]int, len(milestones))
	for _, milestone := range milestones {
		copyOfMilestone := milestone
		groupByMilestone[milestone.Number] = len(groups)
		groups = append(groups, issueGroup{
			ID:        fmt.Sprintf("milestone:%d", milestone.Number),
			Title:     milestone.Title,
			Milestone: &copyOfMilestone,
		})
	}

	noMilestoneIndex := len(groups)
	groups = append(groups, issueGroup{ID: "none", Title: "No milestone"})
	otherMilestoneIndex := -1
	for _, issue := range summary.Issues {
		if issue.Milestone == nil {
			groups[noMilestoneIndex].Issues = append(groups[noMilestoneIndex].Issues, issue)
			continue
		}
		if index, ok := groupByMilestone[issue.Milestone.Number]; ok {
			groups[index].Issues = append(groups[index].Issues, issue)
			continue
		}
		if otherMilestoneIndex < 0 {
			otherMilestoneIndex = len(groups)
			groups = append(groups, issueGroup{ID: "other", Title: "Other milestones"})
		}
		groups[otherMilestoneIndex].Issues = append(groups[otherMilestoneIndex].Issues, issue)
	}

	if len(groups[noMilestoneIndex].Issues) == 0 {
		groups = append(groups[:noMilestoneIndex], groups[noMilestoneIndex+1:]...)
	}
	for index := range groups {
		sort.SliceStable(groups[index].Issues, func(left, right int) bool {
			return groups[index].Issues[left].Number < groups[index].Issues[right].Number
		})
	}
	return groups
}

func (m *Model) View() string {
	switch m.screen {
	case screenResolving:
		return m.renderLoading("Finding a repository...")
	case screenPicker:
		return m.renderPicker()
	case screenLoading:
		return m.renderLoading("Loading issues and milestones...")
	case screenDashboard:
		if m.width >= wideLayoutWidth {
			return m.renderWideDashboard()
		}
		return m.renderNarrowDashboard()
	case screenDetail:
		return m.renderDetail()
	case screenError:
		return m.renderError()
	default:
		return ""
	}
}

func (m *Model) renderLoading(message string) string {
	spinner := spinnerFrames[m.spin%len(spinnerFrames)]
	cardWidth := 52
	if m.width > 0 {
		cardWidth = clamp(m.width-8, 26, cardWidth)
		cardWidth = min(cardWidth, max(1, m.width-2))
	}
	card := lipgloss.NewStyle().
		Width(cardWidth).
		Padding(1, 2).
		Align(lipgloss.Center).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render(
			titleStyle.Render("i-gh") + "\n\n" +
				loadingSpinnerStyle.Render(spinner) + "  " + message + "\n\n" +
				mutedStyle.Render("q: quit"),
		)
	return lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, card)
}

func (m *Model) renderError() string {
	return titleStyle.Render("i-gh") + "\n\n" + errorStyle.Render("Unable to load repository data") +
		"\n\n" + m.errorMessage + "\n\n" + mutedStyle.Render("r: retry   R: choose repository   q: quit")
}

func (m *Model) renderPicker() string {
	var builder strings.Builder
	builder.WriteString(titleStyle.Render("Select repository"))
	builder.WriteString("\n\n")
	if m.pickerFiltering {
		builder.WriteString("Filter: " + m.pickerFilter + "_")
	} else if m.pickerFilter != "" {
		builder.WriteString(mutedStyle.Render("Filter: " + m.pickerFilter))
	} else {
		builder.WriteString(mutedStyle.Render("Choose a repository for i-gh"))
	}
	builder.WriteString("\n\n")

	if m.pickerLoading {
		builder.WriteString("Loading repositories " + spinnerFrames[m.spin%len(spinnerFrames)])
	} else {
		repositories := m.filteredRepositories()
		if len(repositories) == 0 {
			builder.WriteString(mutedStyle.Render("No matching repositories"))
		} else {
			start, end := listWindow(len(repositories), m.repoCursor, max(1, m.height-8))
			for index := start; index < end; index++ {
				repository := repositories[index]
				line := fmt.Sprintf("%s %s", repositoryMarker(index == m.repoCursor), repository.String())
				if repository.Description != "" {
					line += "  " + mutedStyle.Render(truncateText(repository.Description, max(10, m.width-45)))
				}
				if index == m.repoCursor {
					line = selectedStyle.Render(line)
				}
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
	}
	builder.WriteString("\n")
	builder.WriteString(mutedStyle.Render("j/k: move   Enter: select   /: filter   Esc: back   q: quit"))
	return builder.String()
}

func (m *Model) renderHeader() string {
	issues := 0
	milestones := 0
	if m.summary != nil {
		issues = len(m.summary.Issues)
		milestones = len(m.summary.Milestones)
	}
	return titleStyle.Render("i-gh") + "  " + m.repo.String() +
		mutedStyle.Render(fmt.Sprintf("    %d open issues  %d active milestones", issues, milestones))
}

func (m *Model) renderFooter() string {
	var keys string
	switch m.screen {
	case screenDetail:
		keys = "j/k: scroll   c: comments   y: copy   o: browser   w: worktree   r: refresh   Esc: back   q: quit"
	default:
		keys = "j/k: move   h/l: pane   Enter: open   /: filter   r: refresh   R: repository   q: quit"
	}
	footer := mutedStyle.Render(keys)
	if m.status != "" {
		footer += "\n" + statusStyle.Render(m.status)
	}
	return footer
}

func (m *Model) renderWideDashboard() string {
	bodyHeight := max(8, m.height-4)
	leftWidth := clamp(m.width/5, 22, 29)
	rightWidth := clamp(m.width/3, 34, 48)
	middleWidth := m.width - leftWidth - rightWidth
	if middleWidth < 30 {
		rightWidth = max(30, rightWidth-(30-middleWidth))
		middleWidth = m.width - leftWidth - rightWidth
	}

	left := renderPanel("Milestones", m.milestoneLines(max(2, bodyHeight-3)), leftWidth, bodyHeight, m.activePane == paneMilestones)
	middle := renderPanel("Issues", m.issueLines(max(2, bodyHeight-3)), middleWidth, bodyHeight, m.activePane == paneIssues)
	right := renderPanel("Selected issue", m.previewLines(max(2, bodyHeight-3)), rightWidth, bodyHeight, false)
	return m.renderHeader() + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, middle, right) + "\n" + m.renderFooter()
}

func (m *Model) renderNarrowDashboard() string {
	bodyHeight := max(8, m.height-4)
	var title string
	var lines []string
	if m.activePane == paneMilestones {
		title = "Milestones"
		lines = m.milestoneLines(max(2, bodyHeight-3))
	} else {
		title = "Issues"
		lines = m.issueLines(max(2, bodyHeight-3))
	}
	panel := renderPanel(title, lines, max(20, m.width), bodyHeight, true)
	return m.renderHeader() + "\n" + panel + "\n" + m.renderFooter()
}

func (m *Model) milestoneLines(capacity int) []string {
	if len(m.groups) == 0 {
		return []string{"No milestone data"}
	}
	start, end := listWindow(len(m.groups), m.groupCursor, capacity)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		group := m.groups[index]
		name := group.Title
		if group.Milestone != nil {
			name = fmt.Sprintf("#%d %s", group.Milestone.Number, group.Title)
		}
		line := fmt.Sprintf("%s %-24s %d", repositoryMarker(index == m.groupCursor), truncateText(name, 24), len(group.Issues))
		if index == m.groupCursor {
			line = selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *Model) issueLines(capacity int) []string {
	issues := m.filteredIssues()
	lines := make([]string, 0, capacity)
	if m.filtering || m.filter != "" {
		lines = append(lines, mutedStyle.Render("Filter: "+m.filter))
		capacity--
	}
	if len(issues) == 0 {
		lines = append(lines, mutedStyle.Render("No matching open issues"))
		return lines
	}
	if capacity < 1 {
		capacity = 1
	}
	start, end := listWindow(len(issues), m.issueCursor, capacity)
	for index := start; index < end; index++ {
		issue := issues[index]
		line := issueLine(issue, index == m.issueCursor)
		if index == m.issueCursor {
			line = selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *Model) previewLines(capacity int) []string {
	issue, ok := m.selectedIssue()
	if !ok {
		return []string{mutedStyle.Render("Select an issue to preview it")}
	}
	lines := []string{
		fmt.Sprintf("#%d %s", issue.Number, issue.Title),
		"",
		"State: " + issue.State,
		"Comments: " + fmt.Sprint(issue.CommentCount),
	}
	if issue.Milestone != nil {
		lines = append(lines, "Milestone: "+issue.Milestone.Title)
	} else {
		lines = append(lines, "Milestone: none")
	}
	dependencies, dependenciesLoaded := m.dependenciesForIssue(issue)
	if dependenciesLoaded {
		lines = appendDependencyPreview(lines, "Blocked by", dependencies.BlockedBy, issue.BlockedByCount)
		lines = appendDependencyPreview(lines, "Blocking", dependencies.Blocking, issue.BlockingCount)
	} else {
		if issue.BlockedByCount > 0 {
			lines = append(lines, fmt.Sprintf("Blocked by: %d", issue.BlockedByCount))
		}
		if issue.BlockingCount > 0 {
			lines = append(lines, fmt.Sprintf("Blocking: %d", issue.BlockingCount))
		}
		if m.dependencyLoading && m.dependencyNumber == issue.Number {
			lines = append(lines, mutedStyle.Render("Loading dependency details..."))
		} else if m.dependencyError != "" && m.dependencyNumber == issue.Number {
			lines = append(lines, errorStyle.Render("Dependency details unavailable"))
		}
	}
	lines = append(lines, "")
	if m.detail != nil && m.detail.Number == issue.Number {
		lines = append(lines, mutedStyle.Render("Detail loaded. Press Enter to read."))
		if m.detail.Body != "" {
			lines = append(lines, "")
			lines = append(lines, strings.Split(strings.TrimSpace(m.detail.Body), "\n")...)
		}
	} else {
		lines = append(lines, mutedStyle.Render("Press Enter to load the full issue"))
	}
	if len(lines) > capacity {
		lines = lines[:capacity]
	}
	return lines
}

func (m *Model) dependenciesForIssue(issue gh.IssueSummary) (gh.IssueDependencies, bool) {
	if m.detail != nil && m.detail.Number == issue.Number {
		return gh.IssueDependencies{BlockedBy: m.detail.BlockedBy, Blocking: m.detail.Blocking}, true
	}
	dependencies, ok := m.dependencyCache[issue.Number]
	return dependencies, ok
}

func appendDependencyPreview(lines []string, title string, references []gh.IssueReference, fallbackCount int) []string {
	count := len(references)
	if count == 0 {
		count = fallbackCount
	}
	if count == 0 {
		return lines
	}
	lines = append(lines, fmt.Sprintf("%s (%d):", title, count))
	if len(references) == 0 {
		return append(lines, mutedStyle.Render("  No linked issues returned"))
	}
	for _, reference := range references {
		name := reference.Title
		if name == "" {
			name = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf("  #%d %s", reference.Number, name))
	}
	return lines
}

func (m *Model) renderDetail() string {
	header := m.renderHeader()
	if m.detailLoading {
		return header + "\n\nLoading issue #" + fmt.Sprint(m.detailNumber) + " " + spinnerFrames[m.spin%len(spinnerFrames)] + "\n\n" + m.renderFooter()
	}
	if m.detail == nil {
		return header + "\n\n" + errorStyle.Render("Unable to load issue") + "\n\n" + m.detailError + "\n\n" + m.renderFooter()
	}

	contentHeight := max(2, m.height-3)
	lines := m.detailLines()
	m.clampDetailOffset()
	start, end := scrollWindow(len(lines), m.detailOffset, contentHeight)
	content := strings.Join(lines[start:end], "\n")
	if m.detailError != "" {
		content = errorStyle.Render(m.detailError) + "\n\n" + content
	}
	return header + "\n" + content + "\n" + m.renderFooter()
}

func (m *Model) detailLines() []string {
	if m.detail == nil {
		return nil
	}
	markdown := format.IssueMarkdown(m.repo, *m.detail, m.commentsExpanded)
	rendered, err := format.RenderMarkdown(markdown, max(20, m.width-4))
	if err != nil {
		rendered = markdown
	}
	return strings.Split(strings.TrimRight(rendered, "\n"), "\n")
}

func (m *Model) maxDetailOffset() int {
	return max(0, len(m.detailLines())-max(2, m.height-3))
}

func (m *Model) clampDetailOffset() {
	m.detailOffset = clamp(m.detailOffset, 0, m.maxDetailOffset())
}

func issueLine(issue gh.IssueSummary, selected bool) string {
	dependencyMarker := "   "
	if issue.BlockedByCount > 0 {
		dependencyMarker = "(b)"
	}
	metadata := make([]string, 0, 4)
	if issue.CommentCount > 0 {
		metadata = append(metadata, fmt.Sprintf("c%d", issue.CommentCount))
	}
	if issue.BlockedByCount > 0 {
		metadata = append(metadata, fmt.Sprintf("<-%d", issue.BlockedByCount))
	}
	if issue.BlockingCount > 0 {
		metadata = append(metadata, fmt.Sprintf("->%d", issue.BlockingCount))
	}
	for index, label := range issue.Labels {
		if index == 2 {
			break
		}
		metadata = append(metadata, label.Name)
	}
	line := fmt.Sprintf("%s %s #%d %s", repositoryMarker(selected), dependencyMarker, issue.Number, issue.Title)
	if len(metadata) > 0 {
		line += "  [" + strings.Join(metadata, " ") + "]"
	}
	return line
}

func renderPanel(title string, lines []string, width, height int, active bool) string {
	if width < 12 {
		width = 12
	}
	if height < 4 {
		height = 4
	}
	contentWidth := max(1, width-4)
	content := make([]string, 0, len(lines)+1)
	content = append(content, panelTitleStyle.Render(title))
	for _, line := range lines {
		content = append(content, truncateText(line, contentWidth))
	}
	style := lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1).Border(lipgloss.NormalBorder())
	if active {
		style = style.BorderForeground(lipgloss.Color("205"))
	}
	return style.Render(strings.Join(content, "\n"))
}

func listWindow(length, cursor, capacity int) (int, int) {
	if length == 0 {
		return 0, 0
	}
	if capacity < 1 {
		capacity = 1
	}
	cursor = clamp(cursor, 0, length-1)
	start := 0
	if cursor >= capacity {
		start = cursor - capacity + 1
	}
	end := min(length, start+capacity)
	return start, end
}

func scrollWindow(length, offset, capacity int) (int, int) {
	if length == 0 {
		return 0, 0
	}
	if capacity < 1 {
		capacity = 1
	}
	offset = clamp(offset, 0, max(0, length-capacity))
	return offset, min(length, offset+capacity)
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "...")
}

func repositoryMarker(selected bool) string {
	if selected {
		return ">"
	}
	return " "
}

func clamp(value, minimum, maximum int) int {
	if maximum < minimum {
		return minimum
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

var (
	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	panelTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	selectedStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	mutedStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	loadingSpinnerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
)
