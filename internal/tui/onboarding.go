package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var defaultOnboardingTextarea = newOnboardingTextarea()

// onboardingStep tracks which screen the user is on.
type onboardingStep int

const (
	stepProviderType onboardingStep = iota
	stepBaseURL
	stepAPIKey
	stepModel
	stepTheme
	stepDone
)

// selectOption is a label+value pair for list-style steps.
type selectOption struct {
	value string
	label string
}

// OnboardingModel drives the first-run setup wizard.
type OnboardingModel struct {
	step         onboardingStep
	cursor       int
	providerType string
	baseURL      string
	apiKey       string
	model        string
	theme        string
	textInput    textarea.Model
	width        int
	height       int
	quitting     bool
}

// OnboardingResult holds the collected configuration after the wizard finishes.
type OnboardingResult struct {
	ProviderType string
	BaseURL      string
	APIKey       string
	Model        string
	Theme        string
	Completed    bool
}

func newOnboardingTextarea() textarea.Model {
	ti := textarea.New()
	ti.ShowLineNumbers = false
	ti.CharLimit = 256
	ti.SetHeight(1)
	ti.Prompt = ""
	applyOnboardingTextareaTheme(&ti)
	return ti
}

func applyOnboardingTextareaTheme(in *textarea.Model) {
	focused := in.FocusedStyle
	focused.Base = lipgloss.NewStyle()
	focused.CursorLine = lipgloss.NewStyle()
	focused.CursorLineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	focused.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	focused.LineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	focused.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Brand)
	focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)

	blurred := in.BlurredStyle
	blurred.Base = lipgloss.NewStyle()
	blurred.CursorLine = lipgloss.NewStyle()
	blurred.CursorLineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	blurred.LineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	blurred.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)

	in.FocusedStyle = focused
	in.BlurredStyle = blurred
}

// NewOnboardingModel creates a fresh onboarding wizard.
func NewOnboardingModel() OnboardingModel {
	return OnboardingModel{
		step:      stepProviderType,
		cursor:    0,
		textInput: defaultOnboardingTextarea,
	}
}

// Init starts the text input cursor blink.
func (m OnboardingModel) Init() tea.Cmd {
	return textarea.Blink
}

// Result returns the collected values. Completed is true only when the
// user walked through every step without quitting.
func (m OnboardingModel) Result() OnboardingResult {
	return OnboardingResult{
		ProviderType: m.providerType,
		BaseURL:      m.baseURL,
		APIKey:       m.apiKey,
		Model:        m.model,
		Theme:        m.theme,
		Completed:    m.step == stepDone && !m.quitting,
	}
}

// --- option helpers ---

func providerTypeOptions() []selectOption {
	return []selectOption{
		{"openai-codex", "OpenAI Codex (ChatGPT OAuth)"},
		{"openai", "OpenAI"},
		{"anthropic", "Anthropic"},
		{"openai-compatible", "OpenAI-Compatible (third-party)"},
	}
}

func modelOptions(providerType string) []selectOption {
	switch providerType {
	case "openai-codex":
		return []selectOption{
			{"gpt-5.5", "gpt-5.5"},
			{"gpt-5.4", "gpt-5.4"},
			{"gpt-5.4-mini", "gpt-5.4-mini"},
			{"gpt-5.3-codex", "gpt-5.3-codex"},
		}
	case "openai":
		return []selectOption{
			{"gpt-4.1", "gpt-4.1"},
			{"gpt-4.1-mini", "gpt-4.1-mini"},
			{"gpt-4.1-nano", "gpt-4.1-nano"},
		}
	case "anthropic":
		return []selectOption{
			{"claude-sonnet-4-20250514", "claude-sonnet-4-20250514"},
			{"claude-3-5-haiku-latest", "claude-3-5-haiku-latest"},
		}
	default:
		return nil // text input only for openai-compatible
	}
}

func themeOptions() []selectOption {
	return []selectOption{
		{"auto", "Auto (match terminal)"},
		{"dark", "Dark"},
		{"light", "Light"},
	}
}

func defaultBaseURL(providerType string) string {
	switch providerType {
	case "openai-codex":
		return "https://chatgpt.com/backend-api/codex"
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	default:
		return ""
	}
}

// isListStep returns true for steps that show a selectable list.
func (m OnboardingModel) isListStep() bool {
	switch m.step {
	case stepProviderType, stepTheme:
		return true
	case stepModel:
		return modelOptions(m.providerType) != nil
	default:
		return false
	}
}

// currentOptions returns the list options for the active step.
func (m OnboardingModel) currentOptions() []selectOption {
	switch m.step {
	case stepProviderType:
		return providerTypeOptions()
	case stepModel:
		return modelOptions(m.providerType)
	case stepTheme:
		return themeOptions()
	default:
		return nil
	}
}

// Update handles events for the onboarding wizard.
func (m OnboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textInput.SetWidth(min(46, m.width-6))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			return m.goBack()
		}

		if m.isListStep() {
			opts := m.currentOptions()
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
				return m, nil
			case "down", "j":
				if m.cursor < len(opts)-1 {
					m.cursor++
				}
				return m, nil
			case "enter":
				m.selectCurrentOption()
				if m.step == stepDone {
					return m, tea.Quit
				}
				return m, nil
			}
			return m, nil
		}

		// Text input steps: stepBaseURL, stepAPIKey, stepModel (openai-compatible).
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.textInput.Value())
			switch m.step {
			case stepBaseURL:
				m.baseURL = val
				m.step = stepAPIKey
				m.textInput.SetValue("")
				m.textInput.Focus()
			case stepAPIKey:
				m.apiKey = val
				m.step = stepModel
				m.cursor = 0
				if modelOptions(m.providerType) == nil {
					// openai-compatible: text input for model
					m.textInput.SetValue("")
					m.textInput.Focus()
				} else {
					m.textInput.Blur()
				}
			case stepModel:
				// openai-compatible free-text model
				m.model = val
				m.step = stepTheme
				m.cursor = 0
				m.textInput.Blur()
			}
			return m, nil
		}

		// Delegate to textarea for typing.
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	// Pass through other messages (e.g. blink) to textarea.
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// selectCurrentOption commits the highlighted list choice and advances.
func (m *OnboardingModel) selectCurrentOption() {
	switch m.step {
	case stepProviderType:
		opts := providerTypeOptions()
		if m.cursor >= 0 && m.cursor < len(opts) {
			m.providerType = opts[m.cursor].value
		}
		m.baseURL = defaultBaseURL(m.providerType)
		if m.providerType == "openai-codex" {
			m.step = stepModel
			m.cursor = 0
			m.textInput.Blur()
			return
		}
		m.step = stepBaseURL
		m.cursor = 0
		m.textInput.SetValue(m.baseURL)
		m.textInput.Focus()

	case stepModel:
		opts := modelOptions(m.providerType)
		if opts != nil && m.cursor >= 0 && m.cursor < len(opts) {
			m.model = opts[m.cursor].value
		}
		m.step = stepTheme
		m.cursor = 0
		m.textInput.Blur()

	case stepTheme:
		opts := themeOptions()
		if m.cursor >= 0 && m.cursor < len(opts) {
			m.theme = opts[m.cursor].value
		}
		m.step = stepDone
	}
}

// goBack moves to the previous step, or quits if already on the first.
func (m OnboardingModel) goBack() (tea.Model, tea.Cmd) {
	if m.step > stepProviderType {
		if m.providerType == "openai-codex" && m.step == stepModel {
			m.step = stepProviderType
			m.cursor = 0
			m.textInput.Blur()
			return m, nil
		}
		m.step--
		m.cursor = 0
		if m.isListStep() {
			m.textInput.Blur()
		} else {
			// Pre-fill text input with previously entered value.
			switch m.step {
			case stepBaseURL:
				m.textInput.SetValue(m.baseURL)
			case stepAPIKey:
				m.textInput.SetValue(m.apiKey)
			case stepModel:
				m.textInput.SetValue(m.model)
			}
			m.textInput.Focus()
		}
		return m, nil
	}
	m.quitting = true
	return m, tea.Quit
}

// --- view rendering ---

const onboardingCardWidth = 50

// View renders the current onboarding step as a centered card.
func (m OnboardingModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	title, body, hint := m.stepContent()

	// Build card interior.
	innerW := onboardingCardWidth - 4 // account for border padding
	titleStyled := lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.Brand).
		Width(innerW).
		Render(title)

	hintStyled := lipgloss.NewStyle().
		Foreground(currentTheme.Subtle).
		Width(innerW).
		Render(hint)

	content := titleStyled + "\n\n" + body + "\n\n" + hintStyled

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(currentTheme.Border).
		Padding(1, 2).
		Width(onboardingCardWidth).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// stepContent returns the title, body, and footer hint for the current step.
func (m OnboardingModel) stepContent() (title, body, hint string) {
	switch m.step {
	case stepProviderType:
		title = "Choose your provider"
		body = m.renderList(providerTypeOptions())
		hint = "↑↓ navigate · enter select · esc quit"

	case stepBaseURL:
		title = "Base URL"
		body = m.textInput.View()
		hint = "enter confirm · esc back"

	case stepAPIKey:
		title = "API Key"
		body = m.renderMaskedInput()
		hint = "(paste your key, it will be stored securely)\nenter confirm · esc back"

	case stepModel:
		opts := modelOptions(m.providerType)
		if opts != nil {
			title = "Choose model"
			body = m.renderList(opts)
			hint = "↑↓ navigate · enter select · esc back"
		} else {
			title = "Enter your model name"
			body = m.textInput.View()
			hint = "enter confirm · esc back"
		}

	case stepTheme:
		title = "Choose theme"
		body = m.renderList(themeOptions())
		hint = "↑↓ navigate · enter select · esc back"

	default:
		title = "Setup complete"
		body = ""
		hint = ""
	}
	return
}

// renderList renders a vertical list with a cursor indicator.
func (m OnboardingModel) renderList(opts []selectOption) string {
	var b strings.Builder
	for i, opt := range opts {
		if i > 0 {
			b.WriteString("\n")
		}
		if i == m.cursor {
			b.WriteString(lipgloss.NewStyle().
				Foreground(currentTheme.Brand).
				Bold(true).
				Render("> " + opt.label))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(currentTheme.Text).
				Render("  " + opt.label))
		}
	}
	return b.String()
}

// renderMaskedInput shows the textarea value with all but the last 4 chars
// replaced by asterisks, without modifying the actual stored value.
func (m OnboardingModel) renderMaskedInput() string {
	val := m.textInput.Value()
	if len(val) <= 4 {
		return m.textInput.View()
	}
	masked := strings.Repeat("*", len(val)-4) + val[len(val)-4:]
	style := lipgloss.NewStyle().Foreground(currentTheme.Text)
	return style.Render(masked)
}
