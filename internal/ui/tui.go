package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// LogMsg represents a log message to append to the log viewport.
type LogMsg string

// ThinkingMsg represents an AI thinking message to append to the thinking viewport.
type ThinkingMsg string

// InitExecFunc represents the background generation function for init.
type InitExecFunc func(selectedModel string, budget int, aiMode bool, contextData string) (string, error)

// initCompletedMsg represents the message returned when background init execution is complete.
type initCompletedMsg struct {
	guide string
	err   error
}

// Model defines the full-screen Bubble Tea TUI state.
type Model struct {
	thinkingViewport viewport.Model
	logViewport      viewport.Model
	ready            bool
	focused          int // 0 for thinking, 1 for logs

	// Channel to receive retry/resume signals from the TUI.
	RetryChan chan struct{}

	thinkingContent strings.Builder
	logContent      strings.Builder

	// Init flow state
	isInit           bool
	initStep         int // 0: provider, 1: model, 2: budget, 3: mode, 4: questions, 5: executing, 6: finished
	modelMap         map[string][]string
	targetPath       string
	blankProject     bool
	defaultProjName  string
	contextData      string
	selectedProvider string
	selectedModel    string
	budget           int
	aiMode           bool

	// Questionnaire values
	qName     string
	qStack    string
	qDepMgr   string
	qArch     string
	qSecurity string
	qUiUx     string

	form     *huh.Form
	execFunc InitExecFunc
	guide    string
	initErr  error
	Width    int
	Height   int
}

// NewTUIModel creates and initializes a Model with the retry signaling channel.
func NewTUIModel(retryChan chan struct{}) *Model {
	return &Model{
		RetryChan: retryChan,
	}
}

// NewInitTUIModel creates and initializes a Model for the setup/init flow.
func NewInitTUIModel(modelMap map[string][]string, targetPath string, blankProject bool, defaultProjName string, contextData string, execFunc InitExecFunc) *Model {
	return &Model{
		isInit:          true,
		modelMap:        modelMap,
		targetPath:      targetPath,
		blankProject:    blankProject,
		defaultProjName: defaultProjName,
		qName:           defaultProjName,
		contextData:     contextData,
		execFunc:        execFunc,
	}
}

// Init initializes the Bubble Tea program.
func (m *Model) Init() tea.Cmd {
	if m.isInit {
		m.initStep = 0
		m.updateThinkingSummary()
		return m.startInitStep()
	}
	return nil
}

// Update handles state transitions and window resizing.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// 1. Handle TUI-specific messages first (like WindowSizeMsg)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		// Static height overhead for borders, titles, spacers, footer, and the 5-line banner:
		// Header (5 lines) + spacer (1 line) + thinkTitle (1 line) + thinkBox borders (2 lines) 
		// + spacer (1 line) + logTitle (1 line) + logBox borders (2 lines) + spacer (1 line) + footer (1 line) = 15 lines.
		// We budget msg.Height - 19 to leave a 4-line safety margin at the bottom, preventing logo clipping.
		availHeight := msg.Height - 19
		if availHeight < 8 {
			availHeight = 8 // Safeguard minimum size
		}

		// Dynamic distribution: ~35% for Thinking, ~65% for Logs (reducing logs relative height as requested).
		thinkHeight := int(float64(availHeight) * 0.35)
		logHeight := availHeight - thinkHeight

		// Ensure reasonable minimum heights for both viewports
		if thinkHeight < 3 {
			thinkHeight = 3
			logHeight = availHeight - thinkHeight
		}
		if logHeight < 5 {
			logHeight = 5
			thinkHeight = availHeight - logHeight
			if thinkHeight < 3 {
				thinkHeight = 3
			}
		}

		width := msg.Width - 4
		if width < 10 {
			width = 10
		}

		if !m.ready {
			m.thinkingViewport = viewport.New(width, thinkHeight)
			m.logViewport = viewport.New(width, logHeight)
			m.ready = true
		} else {
			m.thinkingViewport.Width = width
			m.thinkingViewport.Height = thinkHeight
			m.logViewport.Width = width
			m.logViewport.Height = logHeight
		}

		m.thinkingViewport.SetContent(m.thinkingContent.String())
		if m.form != nil {
			m.form.WithHeight(m.logViewport.Height)
			m.logViewport.SetContent(m.form.View())
		} else {
			m.logViewport.SetContent(m.logContent.String())
		}
	}

	// 2. If form is active, forward the message to it
	if m.form != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		newForm, cmd := m.form.Update(msg)
		m.form = newForm.(*huh.Form)
		cmds = append(cmds, cmd)

		if m.form.State == huh.StateCompleted {
			nextCmd := m.nextInitStep()
			cmds = append(cmds, nextCmd)
			m.updateThinkingSummary()
			if m.initStep == 5 {
				m.logViewport.SetContent("")
				return m, tea.Batch(m.runInitExecution(), nextCmd)
			}
		} else {
			m.form.WithHeight(m.logViewport.Height)
			m.logViewport.SetContent(m.form.View())
		}
		return m, tea.Batch(cmds...)
	}

	// 3. Handle key messages and event messages when form is NOT active
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			// Disallow focus switching during setup steps
			if !m.isInit || m.initStep >= 5 {
				m.focused = (m.focused + 1) % 2
			}
			return m, nil
		case "enter":
			// Signal background thread that user wants to continue or retry
			select {
			case m.RetryChan <- struct{}{}:
			default:
			}
			return m, nil
		}

		// Direct scrolling keys to the focused viewport
		if m.focused == 0 {
			m.thinkingViewport, cmd = m.thinkingViewport.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			m.logViewport, cmd = m.logViewport.Update(msg)
			cmds = append(cmds, cmd)
		}

	case ThinkingMsg:
		m.thinkingContent.WriteString(string(msg))
		m.thinkingViewport.SetContent(m.thinkingContent.String())
		m.thinkingViewport.GotoBottom()

	case LogMsg:
		m.logContent.WriteString(string(msg))
		m.logViewport.SetContent(m.logContent.String())
		m.logViewport.GotoBottom()

	case initCompletedMsg:
		m.initStep = 6
		m.guide = msg.guide
		m.initErr = msg.err

		if msg.err != nil {
			errBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(Error)).
				Padding(1, 3).
				Render(
					ErrorText.Render("[x] Initialization failed") + "\n\n" +
						BaseText.Render(msg.err.Error()) + "\n\n" +
						MutedText.Render("Press q or ctrl+c to exit."),
				)
			m.logContent.WriteString("\n" + errBox + "\n")
		} else {
			guideStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Text))
			successHeader := SuccessText.Render("[+] Success :: Smidhus Harness initialized successfully!")
			m.logContent.WriteString("\n" + successHeader + "\n\n" + guideStyle.Render(msg.guide) + "\n\n" + MutedText.Render("Press q or ctrl+c to exit.") + "\n")
		}
		m.logViewport.SetContent(m.logContent.String())
		m.logViewport.GotoBottom()
	}

	return m, tea.Batch(cmds...)
}

// View draws the ASCII banner and both viewports side-by-side or stacked.
func (m *Model) View() string {
	if !m.ready {
		return "Initializing Smidhus Harness TUI..."
	}

	header := PrimaryText.Render(`   _____ __  __ ________  __  __  _______    __  _____    ____  _   ________________
  / ___//  |/  //  _/ __ \/ / / / / / ___/   / / / /   |  / __ \/ | / / ____/ ___/ ___/
  \__ \/ /|_/ / / // / / / /_/ / / /\__ \   / /_/ / /| | / /_/ /  |/ / __/  \__ \\__ \
 ___/ / /  / /_/ // /_/ / __  / /_/ /___/  / __  / ___ |/ _, _/ /|  / /___ ___/ /__/ /
/____/_/  /_//___/_____/_/ /_/\____//____/ /_/ /_/_/  |_/_/ |_/_/ |_/_____//____/____/`)

	var thinkBoxStyle, logBoxStyle lipgloss.Style
	var thinkTitle, logTitle string

	if m.isInit && m.initStep < 5 {
		thinkBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Muted)).
			Padding(0, 1)

		logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Primary)).
			Padding(0, 1)

		thinkTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted)).
			Bold(true).
			Render(" CONFIGURATION SUMMARY ")

		logTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(Primary)).
			Bold(true).
			Render(" PROJECT INITIALIZATION SETUP ")
	} else if m.focused == 0 {
		thinkBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Primary)).
			Padding(0, 1)

		logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Muted)).
			Padding(0, 1)

		thinkTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(Primary)).
			Bold(true).
			Render(" THINKING (Reasoning Process - Active) ")

		logTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted)).
			Bold(true).
			Render(" ORCHESTRATION LOGS ")
	} else {
		thinkBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Muted)).
			Padding(0, 1)

		logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(Primary)).
			Padding(0, 1)

		thinkTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Muted)).
			Bold(true).
			Render(" THINKING (Reasoning Process) ")

		logTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(Primary)).
			Bold(true).
			Render(" ORCHESTRATION LOGS - Active ")
	}

	// Enforce static width and height to prevent dynamic expanding/shrinking of borders.
	thinkBoxStyle = thinkBoxStyle.Width(m.thinkingViewport.Width).Height(m.thinkingViewport.Height)
	logBoxStyle = logBoxStyle.Width(m.logViewport.Width).Height(m.logViewport.Height)

	thinkView := thinkBoxStyle.Render(m.thinkingViewport.View())
	logView := logBoxStyle.Render(m.logViewport.View())

	thinkBox := lipgloss.JoinVertical(lipgloss.Left,
		thinkTitle,
		thinkView,
	)

	logBox := lipgloss.JoinVertical(lipgloss.Left,
		logTitle,
		logView,
	)

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(Muted)).
		Italic(true)
	
	footerText := " [Tab] Switch Focus  ·  [↑/↓] Scroll Focused Box  ·  [q] Quit "
	if m.isInit && m.initStep < 5 {
		footerText = " [Tab/Shift+Tab] Navigate  ·  [Enter] Next/Confirm  ·  [ctrl+c] Quit "
	}
	footer := footerStyle.Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		thinkBox,
		"",
		logBox,
		"",
		footer,
	)
}

func (m *Model) nextInitStep() tea.Cmd {
	m.initStep++
	return m.startInitStep()
}

func (m *Model) startInitStep() tea.Cmd {
	var theme = getTheme()

	switch m.initStep {
	case 0:
		var providers []string
		for p := range m.modelMap {
			providers = append(providers, p)
		}
		sort.Strings(providers)

		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Step 1: Select Principal AI Provider").
					Options(huh.NewOptions(providers...)...).
					Value(&m.selectedProvider),
			),
		).WithTheme(theme)

	case 1:
		models := m.modelMap[m.selectedProvider]
		sort.Strings(models)

		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("Step 2: Select Model for %s", m.selectedProvider)).
					Options(huh.NewOptions(models...)...).
					Value(&m.selectedModel),
			),
		).WithTheme(theme)

	case 2:
		budgetOpts := []huh.Option[int]{
			huh.NewOption("Default (Model decides)", 0),
			huh.NewOption("Low (Fast responses)", 1000),
			huh.NewOption("Medium (Balanced)", 4000),
			huh.NewOption("High (Deep reflection)", 10000),
		}

		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[int]().
					Title("Step 3: Reasoning Effort / Thinking Budget").
					Options(budgetOpts...).
					Value(&m.budget),
			),
		).WithTheme(theme)

	case 3:
		if m.blankProject {
			m.form = huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Step 4: Blueprint Generation Mode").
						Description("Do you want the AI to expand and complement the data? (No = Manual literal transcription)").
						Affirmative("Yes, AI Mode").
						Negative("No, Manual").
						Value(&m.aiMode),
				),
			).WithTheme(theme)
		} else {
			m.form = huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Advanced Project Detected").
						Description("This directory contains code. Do you want the AI to scan the architecture? (Warning: Consumes more tokens)").
						Affirmative("Yes, AI Scan").
						Negative("No, Manual Questionnaire").
						Value(&m.aiMode),
				),
			).WithTheme(theme)
		}

	case 4:
		if !m.blankProject && m.aiMode {
			m.initStep = 5
			return m.startInitStep()
		}

		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Project Name").Value(&m.qName).Placeholder(m.defaultProjName),
				huh.NewInput().Title("Main Tech Stack (Language/Framework)").Value(&m.qStack),
				huh.NewInput().Title("Dependency Manager (npm, go modules, pip...)").Value(&m.qDepMgr),
				huh.NewInput().Title("Architecture & Patterns (Clean Architecture, MVC...)").Value(&m.qArch),
				huh.NewInput().Title("Security Requirements (Auth, OWASP...)").Value(&m.qSecurity),
				huh.NewInput().Title("UI/UX Guidelines (Design system, colors - blank if N/A)").Value(&m.qUiUx),
			),
		).WithTheme(theme)

	case 5:
		m.form = nil
		m.ready = true
	}

	if m.form != nil {
		if m.ready {
			m.form.WithHeight(m.logViewport.Height)
		}
		initCmd := m.form.Init()
		m.logViewport.SetContent(m.form.View())
		return initCmd
	}
	return nil
}

func (m *Model) runInitExecution() tea.Cmd {
	return func() tea.Msg {
		var ctxData = m.contextData
		if m.blankProject {
			if m.qName == "" {
				m.qName = m.defaultProjName
			}
			ctxData = fmt.Sprintf(
				"Name: %s\nStack: %s\nDependency Manager: %s\nArchitecture: %s\nSecurity: %s\nUI/UX: %s",
				m.qName, m.qStack, m.qDepMgr, m.qArch, m.qSecurity, m.qUiUx,
			)
		}

		guide, err := m.execFunc(m.selectedModel, m.budget, m.aiMode, ctxData)
		return initCompletedMsg{guide: guide, err: err}
	}
}

func (m *Model) updateThinkingSummary() {
	var sb strings.Builder
	sb.WriteString("┌─ AI Setup Configuration ─────────────────────────────────────────────\n")
	
	// Provider
	if m.selectedProvider != "" {
		sb.WriteString(fmt.Sprintf("│ Provider :: %s\n", m.selectedProvider))
	} else if m.initStep == 0 {
		sb.WriteString("│ Provider :: (selecting...)\n")
	} else {
		sb.WriteString("│ Provider :: (pending...)\n")
	}
	
	// Model
	if m.selectedModel != "" {
		sb.WriteString(fmt.Sprintf("│    Model :: %s\n", m.selectedModel))
	} else if m.initStep == 1 {
		sb.WriteString("│    Model :: (selecting...)\n")
	} else {
		sb.WriteString("│    Model :: (pending...)\n")
	}
	
	// Budget (Reasoning Effort)
	if m.initStep >= 3 {
		budgetStr := "Default (Model decides)"
		if m.budget > 0 {
			budgetStr = fmt.Sprintf("%d ms", m.budget)
		}
		sb.WriteString(fmt.Sprintf("│   Budget :: %s\n", budgetStr))
	} else if m.initStep == 2 {
		sb.WriteString("│   Budget :: (selecting...)\n")
	} else {
		sb.WriteString("│   Budget :: (pending...)\n")
	}
	
	// AI Mode
	if m.initStep >= 4 {
		modeStr := "Manual"
		if m.aiMode {
			if m.blankProject {
				modeStr = "AI Mode"
			} else {
				modeStr = "AI Scan"
			}
		}
		sb.WriteString(fmt.Sprintf("│  AI Mode :: %s\n", modeStr))
	} else if m.initStep == 3 {
		sb.WriteString("│  AI Mode :: (selecting...)\n")
	} else {
		sb.WriteString("│  AI Mode :: (pending...)\n")
	}
	
	sb.WriteString("└───────────────────────────────────────────────────────────────────────\n")

	m.thinkingContent.Reset()
	m.thinkingContent.WriteString(sb.String())
	m.thinkingViewport.SetContent(m.thinkingContent.String())
}

func getTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Title = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(Text))
	t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(Primary)).Bold(true).Padding(0, 1)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary))
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary))
	return t
}

// isBlankProject checks if targetPath contains any actual source code files
func isBlankProject(targetPath string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(targetPath, "*"))
	if err != nil {
		return false, err
	}

	noise := map[string]bool{
		".git":        true,
		".gitignore":  true,
		"README.md":   true,
		"readme.md":   true,
		"LICENSE":     true,
		".DS_Store":   true,
		".harness":    true,
	}

	count := 0
	for _, f := range files {
		base := filepath.Base(f)
		if !noise[base] {
			count++
		}
	}

	return count == 0, nil
}

// InitErr returns the error from background init execution, if any.
func (m *Model) InitErr() error {
	return m.initErr
}
