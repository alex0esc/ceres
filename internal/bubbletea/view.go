package bubbletea

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)


const (
	ThemeColorInfoBar   = lipgloss.Color("#8BE9FD") // Dracula Cyan
	ThemeColorSelected  = lipgloss.Color("#FFB86C") // Dracula Orange
	ThemeColorBorder    = lipgloss.Color("#6272A4") // Dracula Current Line (dezenter Rahmen)
	ThemeColorInactive  = lipgloss.Color("#44475A") // Dracula Comment (gedämpftes Blaugrau)
	ThemeColorSystem    = lipgloss.Color("#40E0D0")
	ThemeColorUser      = lipgloss.Color("#AAAAFF")
)

func (tui *Tui) View() string {
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

// returns style based on focused component
func (tui *Tui) styles() (list lipgloss.Style, info lipgloss.Style, input lipgloss.Style) {
	listBorderColor := lipgloss.Color(ThemeColorInactive)
	inputBorderColor := lipgloss.Color(ThemeColorInactive)
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
		Width(tui.viewport.Width). // gleiche Breite wie Viewport/Input
		Height(1).
		Padding(0, 1)
	input = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(inputBorderColor).
		Padding(0, 1)
	return list, info, input
}


func (tui *Tui) getInfoTextString() string {
	infoColor := lipgloss.NewStyle().Foreground(lipgloss.Color("#FDFD96"))
	client := tui.selectedAgent.Client;
	return infoColor.Render(fmt.Sprintf("Tokens: %v/%v\tStatus: %s", client.TotalTokens, client.CompressionThreshold, tui.selectedAgent.State()))
}
