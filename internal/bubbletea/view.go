package bubbletea

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// paints the whole background in one color
func paint(content string, bg color.Color, w, h int) string {
	if w <= 0 || h <= 0 {
		return content
	}

	r, g, b, _ := bg.RGBA()
	bgSeq := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)

	content = strings.NewReplacer(
		"\x1b[0m", "\x1b[0m"+bgSeq,
		"\x1b[m", "\x1b[m"+bgSeq,
		"\x1b[49m", bgSeq,
	).Replace(content)

	lines := strings.Split(content, "\n")
	out := make([]string, h)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], w, "")
		}
		pad := max(0, w-ansi.StringWidth(line))
		out[i] = bgSeq + line + strings.Repeat(" ", pad) + "\x1b[0m"
	}
	return strings.Join(out, "\n")
}

func (tui *Tui) View() tea.View {
	v := tea.NewView(paint(tui.content(), ThemeColorBackground, tui.width, tui.height))
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
		client.History.TotalTokens,
		client.CompressionThreshold,
		tui.selectedAgent.State(),
	)

	return lipgloss.NewStyle().Foreground(ThemeColorAgentInfo).Render(text)
}
