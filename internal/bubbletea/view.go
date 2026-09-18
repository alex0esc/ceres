package bubbletea

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	ThemeColorInfoBar   = lipgloss.Color("#8BE9FD") // Dracula Cyan
	ThemeColorSelected  = lipgloss.Color("#FFB86C") // Dracula Orange
	ThemeColorBorder    = lipgloss.Color("#6272A4") // Dracula Current Line
	ThemeColorInactive  = lipgloss.Color("#44475A") // Dracula Comment
	ThemeColorSystem    = lipgloss.Color("#40E0D0")
	ThemeColorUser      = lipgloss.Color("#AAAAFF")
	ThemeColorReasoning = lipgloss.Color("#999999")
	ThemeColorAgentInfo = lipgloss.Color("#FDFD96")
)

func (tui *Tui) View() tea.View {
	v := tea.NewView(tui.content())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (tui *Tui) content() string {
	if !tui.ready {
		return "\n  Initializing..."
	}

	listStyle, infoBoxStyle, inputBoxStyle := tui.styles()

	rightPanel := lipgloss.JoinVertical(
		lipgloss.Left,
		tui.viewport.View(),
		infoBoxStyle.Render(tui.getInfoTextString()),
		inputBoxStyle.Render(tui.textarea.View()),
	)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		listStyle.Render(tui.list.View()),
		rightPanel,
	)
}

func (tui *Tui) styles() (list, info, input lipgloss.Style) {
	listBorderColor := ThemeColorInactive
	inputBorderColor := ThemeColorInactive
	if tui.focus == focusList {
		listBorderColor = ThemeColorBorder
	} else {
		inputBorderColor = ThemeColorBorder
	}

	list = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(listBorderColor).
		Width(listWidth)

	info = lipgloss.NewStyle().
		Width(tui.viewport.Width()).
		Height(1).
		Padding(0, 1)

	input = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(inputBorderColor).
		Padding(0, 1)

	return list, info, input
}

func (tui *Tui) getInfoTextString() string {
	if tui.selectedAgent == nil {
		return ""
	}

	client := tui.selectedAgent.Client
	text := fmt.Sprintf(
		"Agent: %s\tTokens: %v/%v\tStatus: %s",
		tui.selectedAgent.Name(),
		client.TotalTokens,
		client.CompressionThreshold,
		tui.selectedAgent.State(),
	)

	return lipgloss.NewStyle().Foreground(ThemeColorAgentInfo).Render(text)
}
