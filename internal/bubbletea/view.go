package bubbletea

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)


const (
	ThemeColorInfo      = lipgloss.Color("#8BE9FD") // Dracula Cyan
	ThemeColorUser      = lipgloss.Color("#FFB86C") // Dracula Orange
	ThemeColorBorder    = lipgloss.Color("#6272A4") // Dracula Current Line (dezenter Rahmen)
	ThemeColorInactive  = lipgloss.Color("#44475A") // Dracula Comment (gedämpftes Blaugrau)
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
		inputBoxStyle.Render(tui.textinput.View()),
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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(listBorderColor).
		Width(listWidth)
	info = lipgloss.NewStyle().
		Width(tui.viewport.Width). // gleiche Breite wie Viewport/Input
		Height(1).
		Padding(0, 1)
	input = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor).
		Padding(0, 1)
	return list, info, input
}


func (tui *Tui) getInfoTextString() string {
	infoColor := lipgloss.NewStyle().Foreground(lipgloss.Color("#FDFD96"))
	return infoColor.Render(fmt.Sprintf("Tokens: %v", tui.selectedAgent.Client.TotalTokens))

}
