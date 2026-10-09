package bubbletea

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alex0esc/ceres/pkg/handles"
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
		"",
		tui.viewport.View(),
		"",
		infoBoxStyle.Render(tui.getInfoTextString()),
		inputBoxStyle.Render(tui.textarea.View()),
	)

	return lipgloss.NewStyle().
		Padding(0, 1).
		Render(lipgloss.JoinHorizontal(
			lipgloss.Top,
			listStyle.Render(tui.list.View()),
			rightPanel,
		))
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
	used := client.History.TotalTokens
	threshold := client.CompressionThreshold

	// status: spinner while working, solid marker otherwise
	var status string
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	switch {
	case client.IsCompressing():
		status = spinner[tui.tickFrame%len(spinner)] + " Compressing"
	case tui.selectedAgent.State() == handles.AgentStateBusy:
		status = spinner[tui.tickFrame%len(spinner)] + " Busy"
	case tui.selectedAgent.State() == handles.AgentStateIdle:
		status = "▣ Idle"
	case tui.selectedAgent.State() == handles.AgentStateStopped:
		status = "▣ Stopped"
	default:
		status = tui.selectedAgent.State().String()
	}

	infoStyle := lipgloss.NewStyle().Foreground(ThemeColorAgentInfo)
	sep := infoStyle.Render("   •   ")

	statusSeg := infoStyle.Render(status)
	barSeg := infoStyle.Render(progressBar(used, threshold, 20) + " " + formatK(used))
	agent := infoStyle.Copy().Bold(true).Render(tui.selectedAgent.Name())

	text := agent + sep + barSeg + sep + statusSeg

	// keep it single line: the info style has Padding(0, 1), so the usable
	// inner width is the viewport width minus the padding plus a small safety
	// margin so the tail never wraps to the next line.
	inner := max(tui.viewport.Width()-4, 0)
	text = ansi.Truncate(text, inner, "…")

	return text
}

// formatK renders a token count in thousands with a k suffix (e.g. 26000 -> 26k).
func formatK(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%dk", n)
	}
	return fmt.Sprintf("%dk", (n+500)/1000)
}

// progressBar renders a fixed-width usage bar filled relative to threshold.
func progressBar(used, threshold int64, width int) string {
	filled := 0
	if threshold > 0 {
		filled = int(float64(used) / float64(threshold) * float64(width))
		filled = min(max(filled, 0), width)
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
