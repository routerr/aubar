package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/routerr/aubar/internal/auth"
	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/keyringx"
)

var ErrAborted = errors.New("aborted")

var providerOrder = []string{"openai", "claude", "gemini"}

type screen string

const (
	screenWelcome       screen = "welcome"
	screenProviderIntro screen = "provider_intro"
	screenAuthChoice    screen = "auth_choice"
	screenOAuthGuide    screen = "oauth_guide"
	screenKeyInput      screen = "key_input"
	screenKeyResult     screen = "key_result"
	screenSourceOrder   screen = "source_order"
	screenRefresh       screen = "refresh"
	screenTmuxEnable    screen = "tmux_enable"
	screenTmuxPosition  screen = "tmux_position"
	screenSummary       screen = "summary"
)

type option struct {
	Label       string
	Description string
}

type historyEntry struct {
	Screen      screen
	ProviderIdx int
}

type model struct {
	cfg            config.Settings
	settingsPath   string
	screen         screen
	providerIdx    int
	choiceIdx      int
	history        []historyEntry
	input          textinput.Model
	inputMode      bool
	authChoice     map[string]string
	pendingSecrets map[string]string
	status         string
	validation     auth.Result
	completed      bool
	aborted        bool
}

func Run(cfg config.Settings, settingsPath string) (config.Settings, error) {
	m := newModel(cfg, settingsPath)
	p := tea.NewProgram(m, tea.WithAltScreen())
	out, err := p.Run()
	if err != nil {
		return cfg, err
	}
	final := out.(model)
	if final.aborted || !final.completed {
		return cfg, ErrAborted
	}
	for providerName, secret := range final.pendingSecrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		if err := keyringx.Set(providerName, secret); err != nil {
			return cfg, fmt.Errorf("save %s credential: %w", providerName, err)
		}
	}
	return final.cfg, nil
}

func newModel(cfg config.Settings, settingsPath string) model {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Width = 56
	return model{
		cfg:            cfg,
		settingsPath:   settingsPath,
		screen:         screenWelcome,
		input:          ti,
		authChoice:     map[string]string{},
		pendingSecrets: map[string]string{},
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if focus, ok := msg.(focusInputMsg); ok {
		m.input.SetValue(string(focus))
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
		m.input.Focus()
		m.inputMode = true
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.inputMode {
			switch msg.String() {
			case "ctrl+c", "q":
				m.aborted = true
				return m, tea.Quit
			case "esc":
				m.inputMode = false
				m.input.Blur()
				m.status = "key entry cancelled"
				return m, nil
			case "enter":
				return m.submitKey()
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		case "up", "k":
			if m.choiceIdx > 0 {
				m.choiceIdx--
			}
			return m, nil
		case "down", "j":
			if m.choiceIdx < len(m.options())-1 {
				m.choiceIdx++
			}
			return m, nil
		case "esc", "left", "h":
			m.goBack()
			return m, nil
		case "enter":
			return m.choose()
		}
	}
	return m, nil
}

func (m model) View() string {
	styles := newThemeStyles()
	header := styles.header.Render("AUBAR Setup Wizard")
	sub := styles.meta.Render(m.progressLabel())
	body := styles.panel.Width(110).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			styles.title.Render(m.title()),
			styles.copy.Render(m.body()),
			m.renderOptions(styles),
			m.renderInput(styles),
			m.renderHelp(styles),
		),
	)
	status := styles.status.Render(m.status)
	footer := styles.footer.Render("Keys: up/down choose • enter continue • esc back • q quit")
	return lipgloss.JoinVertical(lipgloss.Left, header, sub, body, status, footer)
}

func (m model) renderOptions(styles themeStyles) string {
	if m.inputMode {
		return ""
	}
	opts := m.options()
	if len(opts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(opts))
	for i, opt := range opts {
		prefix := "  "
		style := styles.row
		if i == m.choiceIdx {
			prefix = "▸ "
			style = styles.rowSelected
		}
		lines = append(lines, style.Render(prefix+opt.Label+"  "+opt.Description))
	}
	return styles.section.Render(strings.Join(lines, "\n"))
}

func (m model) renderInput(styles themeStyles) string {
	if !m.inputMode {
		return ""
	}
	return styles.editor.Render("Credential> " + m.input.View())
}

func (m model) renderHelp(styles themeStyles) string {
	lines := m.helpLines()
	if len(lines) == 0 {
		return ""
	}
	return styles.help.Render(strings.Join(lines, "\n"))
}

func (m model) choose() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenWelcome:
		return m.advance(screenProviderIntro), nil
	case screenProviderIntro:
		return m.handleProviderIntro()
	case screenAuthChoice:
		return m.handleAuthChoice()
	case screenOAuthGuide:
		if m.choiceIdx == 0 {
			return m.advance(screenKeyInput), m.focusInput("")
		}
		m.disableCurrentProvider()
		return m.advanceToNextProvider(), nil
	case screenKeyResult:
		if m.validation.OK && m.choiceIdx == 0 {
			m.authChoice[currentProvider(m.providerIdx)] = "api"
			m.pendingSecrets[currentProvider(m.providerIdx)] = strings.TrimSpace(m.input.Value())
			return m.advance(screenSourceOrder), nil
		}
		if !m.validation.OK && m.choiceIdx == 0 {
			return m.advance(screenKeyInput), m.focusInput("")
		}
		if m.validation.OK {
			return m.advance(screenKeyInput), m.focusInput(m.input.Value())
		}
		m.disableCurrentProvider()
		return m.advanceToNextProvider(), nil
	case screenSourceOrder:
		m.applySourceOrder(m.choiceIdx)
		return m.advanceToNextProvider(), nil
	case screenRefresh:
		m.applyRefresh(m.choiceIdx)
		if m.cfg.Tmux.Enabled {
			return m.advance(screenTmuxEnable), nil
		}
		return m.advance(screenTmuxEnable), nil
	case screenTmuxEnable:
		m.cfg.Tmux.Enabled = m.choiceIdx == 0
		if m.cfg.Tmux.Enabled {
			return m.advance(screenTmuxPosition), nil
		}
		return m.advance(screenSummary), nil
	case screenTmuxPosition:
		if m.choiceIdx == 0 {
			m.cfg.Tmux.Position = "top"
		} else {
			m.cfg.Tmux.Position = "bottom"
		}
		return m.advance(screenSummary), nil
	case screenSummary:
		if m.choiceIdx == 0 {
			m.completed = true
			return m, tea.Quit
		}
		m.aborted = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) submitKey() (tea.Model, tea.Cmd) {
	secret := strings.TrimSpace(m.input.Value())
	if secret == "" {
		m.status = "credential cannot be empty"
		return m, nil
	}
	result := auth.ValidateCredential(context.Background(), currentProvider(m.providerIdx), secret)
	m.validation = result
	m.inputMode = false
	m.input.Blur()
	if result.OK {
		if result.Warning != "" {
			m.status = result.Warning
		} else {
			m.status = "credential validated"
		}
	} else {
		m.status = result.Message
	}
	return m.advance(screenKeyResult), nil
}

func (m model) handleProviderIntro() (tea.Model, tea.Cmd) {
	name := currentProvider(m.providerIdx)
	if m.choiceIdx == 0 {
		p := m.cfg.Providers[name]
		p.Enabled = true
		m.cfg.Providers[name] = p
		return m.advance(screenAuthChoice), nil
	}
	m.disableCurrentProvider()
	return m.advanceToNextProvider(), nil
}

func (m model) handleAuthChoice() (tea.Model, tea.Cmd) {
	name := currentProvider(m.providerIdx)
	switch m.choiceIdx {
	case 0:
		// CLI chosen
		m.authChoice[name] = "cli"
		p := m.cfg.Providers[name]
		p.Enabled = true
		p.SourceOrder = []string{"cli"}
		m.cfg.Providers[name] = p
		m.status = fmt.Sprintf("%s will use local CLI", providerTitle(name))
		return m.advance(screenSourceOrder), nil
	case 1:
		// API Key chosen
		m.authChoice[name] = "api"
		return m.advance(screenKeyInput), m.focusInput("")
	default:
		m.disableCurrentProvider()
		return m.advanceToNextProvider(), nil
	}
}

func (m *model) goBack() {
	if m.inputMode {
		m.inputMode = false
		m.input.Blur()
		return
	}
	if len(m.history) == 0 {
		return
	}
	last := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	m.screen = last.Screen
	m.providerIdx = last.ProviderIdx
	m.choiceIdx = 0
	m.status = ""
}

func (m model) advance(next screen) model {
	m.history = append(m.history, historyEntry{Screen: m.screen, ProviderIdx: m.providerIdx})
	m.screen = next
	m.choiceIdx = 0
	m.status = ""
	return m
}

func (m model) advanceToNextProvider() model {
	if m.providerIdx < len(providerOrder)-1 {
		m.providerIdx++
		return m.advance(screenProviderIntro)
	}
	return m.advance(screenRefresh)
}

func (m *model) disableCurrentProvider() {
	name := currentProvider(m.providerIdx)
	p := m.cfg.Providers[name]
	p.Enabled = false
	m.cfg.Providers[name] = p
	delete(m.authChoice, name)
	delete(m.pendingSecrets, name)
}

func (m *model) applySourceOrder(idx int) {
	name := currentProvider(m.providerIdx)
	p := m.cfg.Providers[name]
	switch idx {
	case 0:
		p.SourceOrder = []string{"cli"}
	case 1:
		p.SourceOrder = []string{"api"}
	case 2:
		p.SourceOrder = []string{"api", "cli"}
	case 3:
		p.SourceOrder = []string{"cli", "api"}
	}
	m.cfg.Providers[name] = p
}

func (m *model) applyRefresh(idx int) {
	switch idx {
	case 0:
		m.cfg.Refresh.GlobalIntervalSeconds = 30
	case 1:
		m.cfg.Refresh.GlobalIntervalSeconds = 60
	default:
		m.cfg.Refresh.GlobalIntervalSeconds = 120
	}
}

func (m model) progressLabel() string {
	switch m.screen {
	case screenWelcome:
		return "Step 1 of 7"
	case screenProviderIntro, screenAuthChoice, screenOAuthGuide, screenKeyInput, screenKeyResult, screenSourceOrder:
		return fmt.Sprintf("Provider %d of %d", m.providerIdx+1, len(providerOrder))
	case screenRefresh:
		return "Refresh settings"
	case screenTmuxEnable, screenTmuxPosition:
		return "Tmux settings"
	case screenSummary:
		return "Review and save"
	default:
		return ""
	}
}

func (m model) title() string {
	switch m.screen {
	case screenWelcome:
		return "Sequential setup for provider access"
	case screenProviderIntro:
		return fmt.Sprintf("Enable %s?", providerTitle(currentProvider(m.providerIdx)))
	case screenAuthChoice:
		return fmt.Sprintf("Choose %s authentication", providerTitle(currentProvider(m.providerIdx)))
	case screenOAuthGuide:
		return "Gemini OAuth guidance"
	case screenKeyInput:
		return fmt.Sprintf("Enter %s credential", providerTitle(currentProvider(m.providerIdx)))
	case screenKeyResult:
		return fmt.Sprintf("%s validation result", providerTitle(currentProvider(m.providerIdx)))
	case screenSourceOrder:
		return fmt.Sprintf("%s data source order", providerTitle(currentProvider(m.providerIdx)))
	case screenRefresh:
		return "Refresh cadence"
	case screenTmuxEnable:
		return "Enable tmux status integration?"
	case screenTmuxPosition:
		return "Choose tmux status position"
	case screenSummary:
		return "Ready to save"
	default:
		return "Aubar setup"
	}
}

func (m model) body() string {
	switch m.screen {
	case screenWelcome:
		return "This wizard asks one question at a time, validates provider credentials live, and saves working settings plus encrypted secrets for Aubar."
	case screenProviderIntro:
		return fmt.Sprintf("Enable %s quota checks in Aubar. You can skip any provider and add it later.", providerTitle(currentProvider(m.providerIdx)))
	case screenAuthChoice:
		return fmt.Sprintf("Pick how Aubar should authenticate to %s for usage retrieval.", providerTitle(currentProvider(m.providerIdx)))
	case screenOAuthGuide:
		return "Google Gemini has official OAuth support, but direct OAuth integration inside Aubar is not implemented in v1 because it requires a registered OAuth client/callback flow. API key setup is supported now."
	case screenKeyInput:
		return keyPrompt(currentProvider(m.providerIdx))
	case screenKeyResult:
		if m.validation.OK {
			return "Credential probe succeeded. You can save it now or re-enter it."
		}
		return "Credential probe failed. Review the provider-specific guidance below, then retry or skip."
	case screenSourceOrder:
		return "Choose whether Aubar should try provider APIs or local CLIs first when fetching usage."
	case screenRefresh:
		return "Select how often the background updater should refresh."
	case screenTmuxEnable:
		return "Aubar can keep a cached line ready for tmux status-right."
	case screenTmuxPosition:
		return "Pick where tmux should place its status bar."
	case screenSummary:
		return summaryBody(m.cfg, m.pendingSecrets)
	default:
		return ""
	}
}

func (m model) options() []option {
	switch m.screen {
	case screenWelcome:
		return []option{{Label: "Start setup", Description: "Guide me through credentials and tmux settings"}}
	case screenProviderIntro:
		return []option{
			{Label: "Enable (Recommended)", Description: "Configure this provider now"},
			{Label: "Skip", Description: "Leave it disabled for now"},
		}
	case screenAuthChoice:
		if currentProvider(m.providerIdx) == "gemini" {
			return []option{
				{Label: "Gemini CLI (Recommended)", Description: "Use Aubar's built-in Gemini collector with your local Gemini CLI OAuth credentials"},
				{Label: "API key", Description: "Use an explicit Gemini API key"},
				{Label: "Skip", Description: "Leave Gemini disabled for now"},
			}
		}
		if currentProvider(m.providerIdx) == "openai" {
			return []option{
				{Label: "Codex CLI (Recommended)", Description: "Use local codex usage --json tool without requiring an Admin API key"},
				{Label: "API key", Description: "For OpenAI organization usage/cost data with Admin API access"},
				{Label: "Skip", Description: "Leave this provider disabled for now"},
			}
		}
		if currentProvider(m.providerIdx) == "claude" {
			return []option{
				{Label: "Claude Code CLI (Recommended)", Description: "Use Aubar's built-in Claude collector with local Claude Code credentials"},
				{Label: "API key", Description: "For Anthropic Usage & Cost Admin API access"},
				{Label: "Skip", Description: "Leave this provider disabled for now"},
			}
		}
		return []option{
			{Label: "Local CLI (Recommended)", Description: "Use local CLI tool without requiring a new API key"},
			{Label: "API key", Description: "Required for direct provider data retrieval"},
			{Label: "Skip", Description: "Leave this provider disabled for now"},
		}
	case screenOAuthGuide:
		return []option{
			{Label: "Use API key instead", Description: "Continue with the supported v1 path"},
			{Label: "Skip Gemini", Description: "Leave Gemini disabled for now"},
		}
	case screenKeyResult:
		if m.validation.OK {
			return []option{
				{Label: "Save and continue (Recommended)", Description: "Store this credential in keyring and move on"},
				{Label: "Re-enter key", Description: "Input a different credential"},
			}
		}
		return []option{
			{Label: "Retry key entry (Recommended)", Description: "Enter another credential"},
			{Label: "Skip provider", Description: "Leave this provider disabled for now"},
		}
	case screenSourceOrder:
		return []option{
			{Label: "CLI only (Recommended)", Description: "Use local CLI for quota/usage retrieval"},
			{Label: "API only", Description: "Use official provider APIs"},
			{Label: "API then CLI", Description: "Try official APIs first, then local CLI"},
			{Label: "CLI then API", Description: "Try local CLI first, then official APIs"},
		}
	case screenRefresh:
		return []option{
			{Label: "30 seconds (Recommended)", Description: "Fastest refresh Aubar is designed around"},
			{Label: "60 seconds", Description: "Safer for providers with stricter admin endpoints"},
			{Label: "120 seconds", Description: "Low-noise background mode"},
		}
	case screenTmuxEnable:
		return []option{
			{Label: "Enable tmux integration (Recommended)", Description: "Keep a cached banner ready for tmux"},
			{Label: "Disable tmux integration", Description: "Use Aubar from the terminal only"},
		}
	case screenTmuxPosition:
		return []option{
			{Label: "Top (Recommended)", Description: "Best fit for the banner-style status layout"},
			{Label: "Bottom", Description: "Use tmux's normal bottom status position"},
		}
	case screenSummary:
		return []option{
			{Label: "Save and finish (Recommended)", Description: "Write settings and encrypted secrets"},
			{Label: "Abort", Description: "Exit without saving changes"},
		}
	default:
		return nil
	}
}

func (m model) helpLines() []string {
	switch m.screen {
	case screenKeyInput:
		return providerHelp(currentProvider(m.providerIdx))
	case screenKeyResult:
		lines := []string{}
		if m.validation.Message != "" {
			lines = append(lines, "Result: "+m.validation.Message)
		}
		if m.validation.Warning != "" {
			lines = append(lines, "Note: "+m.validation.Warning)
		}
		lines = append(lines, providerHelp(currentProvider(m.providerIdx))...)
		return lines
	case screenOAuthGuide:
		return []string{
			"OpenAI API and Anthropic Admin API do not expose the equivalent OAuth path Aubar would need for usage retrieval.",
			"Gemini official OAuth docs: https://ai.google.dev/gemini-api/docs/oauth",
			"Gemini API key page: https://aistudio.google.com/app/apikey",
		}
	default:
		return nil
	}
}

func providerHelp(provider string) []string {
	help := auth.ProviderHelp(provider)
	lines := []string{}
	if help.GetKeyURL != "" {
		lines = append(lines, "Get key: "+help.GetKeyURL)
	}
	if help.DocsURL != "" {
		lines = append(lines, "Docs: "+help.DocsURL)
	}
	for _, step := range help.Instructions {
		lines = append(lines, "How: "+step)
	}
	return lines
}

func keyPrompt(provider string) string {
	switch provider {
	case "openai":
		return "Paste your OpenAI credential. Aubar validates it against the organization usage/cost endpoint, which typically requires an OpenAI Admin API key with organization-owner access."
	case "claude":
		return "Paste your Anthropic Admin API key. Aubar validates it against the Usage & Cost API, which requires a key starting with sk-ant-admin."
	case "gemini":
		return "Paste your Gemini API key from Google AI Studio. Aubar validates it against the Gemini models endpoint."
	default:
		return "Paste your provider credential."
	}
}

func summaryBody(cfg config.Settings, pendingSecrets map[string]string) string {
	lines := []string{
		"Setup summary:",
		fmt.Sprintf("OpenAI: %s", providerSummary("openai", cfg, pendingSecrets)),
		fmt.Sprintf("Claude: %s", providerSummary("claude", cfg, pendingSecrets)),
		fmt.Sprintf("Gemini: %s", providerSummary("gemini", cfg, pendingSecrets)),
		fmt.Sprintf("Refresh: every %d seconds", cfg.Refresh.GlobalIntervalSeconds),
	}
	if cfg.Tmux.Enabled {
		lines = append(lines, fmt.Sprintf("tmux: enabled (%s)", cfg.Tmux.Position))
		lines = append(lines, "")
		lines = append(lines, "tmux instructions:")
		lines = append(lines, fmt.Sprintf("set -g status-position %s", cfg.Tmux.Position))
		lines = append(lines, fmt.Sprintf("set -g status-right '#(cat %s 2>/dev/null || echo \"AUBAR booting\")'", cfg.Tmux.StatusFile))
		lines = append(lines, "Then run: aubar run")
	} else {
		lines = append(lines, "tmux: disabled")
	}
	return strings.Join(lines, "\n")
}

func providerSummary(name string, cfg config.Settings, pendingSecrets map[string]string) string {
	p := cfg.Providers[name]
	if !p.Enabled {
		return "disabled"
	}
	source := strings.Join(p.SourceOrder, " -> ")
	if _, ok := pendingSecrets[name]; ok {
		return "enabled, validated key pending save, source " + source
	}
	return "enabled, existing key/env expected, source " + source
}

func providerTitle(name string) string {
	switch name {
	case "openai":
		return "OpenAI"
	case "claude":
		return "Claude"
	case "gemini":
		return "Gemini"
	default:
		return strings.Title(name)
	}
}

func currentProvider(idx int) string {
	if idx < 0 || idx >= len(providerOrder) {
		return providerOrder[0]
	}
	return providerOrder[idx]
}

type themeStyles struct {
	header      lipgloss.Style
	title       lipgloss.Style
	meta        lipgloss.Style
	panel       lipgloss.Style
	copy        lipgloss.Style
	section     lipgloss.Style
	row         lipgloss.Style
	rowSelected lipgloss.Style
	editor      lipgloss.Style
	help        lipgloss.Style
	status      lipgloss.Style
	footer      lipgloss.Style
}

func newThemeStyles() themeStyles {
	bg := lipgloss.Color("#111418")
	panelBg := lipgloss.Color("#1A1F24")
	accent := lipgloss.Color("#8FD3C1")
	text := lipgloss.Color("#E6E4DF")
	muted := lipgloss.Color("#98A2B3")
	active := lipgloss.Color("#D4B483")

	return themeStyles{
		header: lipgloss.NewStyle().
			Foreground(text).
			Background(bg).
			Bold(true).
			Padding(0, 1),
		title: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		meta: lipgloss.NewStyle().
			Foreground(accent).
			Padding(0, 0, 1, 0),
		panel: lipgloss.NewStyle().
			Background(panelBg).
			Foreground(text).
			Padding(1, 2).
			MarginTop(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent),
		copy: lipgloss.NewStyle().
			Foreground(text).
			Padding(0, 0, 1, 0),
		section: lipgloss.NewStyle().
			Padding(1, 0),
		row: lipgloss.NewStyle().
			Foreground(text),
		rowSelected: lipgloss.NewStyle().
			Foreground(active).
			Bold(true),
		editor: lipgloss.NewStyle().
			Foreground(text).
			Background(bg).
			Padding(0, 1).
			MarginTop(1),
		help: lipgloss.NewStyle().
			Foreground(muted).
			Padding(1, 0, 0, 0),
		status: lipgloss.NewStyle().
			Foreground(accent).
			Padding(1, 0, 0, 0),
		footer: lipgloss.NewStyle().
			Foreground(muted).
			Padding(1, 0, 0, 0),
	}
}

type focusInputMsg string

func (m model) focusInput(value string) tea.Cmd {
	return func() tea.Msg { return focusInputMsg(value) }
}
