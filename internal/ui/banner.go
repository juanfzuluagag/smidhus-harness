package ui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
)

// Dark Forge Palette
const (
	Primary = "#FF5A00" // Ember Orange
	Text    = "#E5E7EB" // Steel Light
	Success = "#059669" // Tempered Green
	Error   = "#DC2626" // Molten Red
	Warning = "#D97706" // Amber
	Muted   = "#6B7280" // Ash Gray
)

var (
	// Base styles
	BaseText    = lipgloss.NewStyle().Foreground(lipgloss.Color(Text))
	PrimaryText = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
	SuccessText = lipgloss.NewStyle().Foreground(lipgloss.Color(Success)).Bold(true)
	ErrorText   = lipgloss.NewStyle().Foreground(lipgloss.Color(Error)).Bold(true)
	WarningText = lipgloss.NewStyle().Foreground(lipgloss.Color(Warning)).Bold(true)
	MutedText   = lipgloss.NewStyle().Foreground(lipgloss.Color(Muted))

	// Key-Value layout uses fixed-width keys for optical alignment in output logs.
	KeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(Muted)).Width(12).Align(lipgloss.Right)
	ValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Bold(true)
)

// PrintBanner prints the main application banner
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

// PrintSuccess prints a success message using industrial brackets
func PrintSuccess(msg string) {
	fmt.Println(SuccessText.Render("[+] ") + BaseText.Render(msg))
}

// PrintError prints an error message using industrial brackets
func PrintError(msg string) {
	fmt.Println(ErrorText.Render("[x] ") + BaseText.Render(msg))
}

// PrintWarning prints a warning message using industrial brackets
func PrintWarning(msg string) {
	fmt.Println(WarningText.Render("[!] ") + BaseText.Render(msg))
}

// PrintInfo prints a neutral info message using industrial brackets
func PrintInfo(msg string) {
	fmt.Println(PrimaryText.Render(" >  ") + BaseText.Render(msg))
}

// PrintKeyValue renders a clean Key-Value row with a rigid divider
func PrintKeyValue(key, value string) {
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color(Muted)).Render(" :: ")
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, KeyStyle.Render(key), divider, ValueStyle.Render(value)))
}

// PrintStep renders a minimalist progress step
func PrintStep(step, detail string) {
	stepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Primary)).Width(12).Align(lipgloss.Right).Bold(true)
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color(Muted)).Render(" █ ")
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, stepStyle.Render(step), divider, BaseText.Render(detail)))
}

// RenderMessage renders a simple message in steel text
func RenderMessage(msg string) string {
	return BaseText.Render(msg)
}