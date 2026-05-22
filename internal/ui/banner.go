package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Coco Clean Palette
const (
	Primary = "#5E35B1" // Deep Purple
	Text    = "#4B5563" // Clean Slate
	Success = "#10B981" // Emerald Green
	Error   = "#DC2626" // Crimson Red
	Warning = "#F59E0B" // Amber
	Muted   = "#9CA3AF" // Cool Gray
)

var (
	// Base styles
	BaseText    = lipgloss.NewStyle().Foreground(lipgloss.Color(Text))
	PrimaryText = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
	SuccessText = lipgloss.NewStyle().Foreground(lipgloss.Color(Success))
	ErrorText   = lipgloss.NewStyle().Foreground(lipgloss.Color(Error))
	WarningText = lipgloss.NewStyle().Foreground(lipgloss.Color(Warning))
	MutedText   = lipgloss.NewStyle().Foreground(lipgloss.Color(Muted))

	// Key-Value styles
	KeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(Muted)).Width(10)
	ValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
)

// PrintBanner prints the main application banner in a clean Slant ASCII font
func PrintBanner() {
	banner := `
   _____ __  __ ________  __  __  _______    __  _____    ____  _   ________________ 
  / ___//  |/  //  _/ __ \/ / / / / / ___/   / / / /   |  / __ \/ | / / ____/ ___/ ___/
  \__ \/ /|_/ / / // / / / /_/ / / /\__ \   / /_/ / /| | / /_/ /  |/ / __/  \__ \\__ \ 
 ___/ / /  / /_/ // /_/ / __  / /_/ /___/  / __  / ___ |/ _, _/ /|  / /___ ___/ /__/ / 
/____/_/  /_//___/_____/_/ /_/\____//____/ /_/ /_/_/  |_/_/ |_/_/ |_/_____//____/____/ 
`
	fmt.Println(PrimaryText.Render(banner))
}

// PrintSuccess prints a success message with an icon
func PrintSuccess(msg string) {
	fmt.Println(SuccessText.Render("✅ " + msg))
}

// PrintError prints an error message with an icon
func PrintError(msg string) {
	fmt.Println(ErrorText.Render("❌ " + msg))
}

// PrintWarning prints a warning message with an icon
func PrintWarning(msg string) {
	fmt.Println(WarningText.Render("⚠️  " + msg))
}

// PrintInfo prints a neutral info message with an icon
func PrintInfo(msg string) {
	fmt.Println(PrimaryText.Render("ℹ️  " + msg))
}

// PrintKeyValue renders a clean Key-Value row
func PrintKeyValue(key, value string) {
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, KeyStyle.Render(key), ValueStyle.Render(value)))
}

// PrintStep renders a minimalist progress step (e.g., Task      │ Doing something)
func PrintStep(step, detail string) {
	stepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Width(10).Bold(true)
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color(Muted)).Render(" │ ")
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, stepStyle.Render(step), divider, BaseText.Render(detail)))
}

// RenderMessage renders a simple message in clean slate text
func RenderMessage(msg string) string {
	return BaseText.Render(msg)
}