package bubbletea

import (
	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/server"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// item type in the selectalbe list
type listItem struct {
	botName string
	botDesc string
}

func (item listItem) FilterValue() string { return item.botName }
func (item listItem) Title() string       { return item.botName }
func (item listItem) Description() string { return item.botDesc }

const (
	listWidth    = 40
	footerHeight = 7 // Inputbox: border top + bottom (4 + 3)
)

func newListDelegate() list.ItemDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(1)
	delegate.SetHeight(10)
	delegate.ShowDescription = true

	// Selektiertes Item: Orange, mit linkem Balken
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(ThemeColorSelected).
		BorderForeground(ThemeColorSelected)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(ThemeColorSelected).
		BorderForeground(ThemeColorSelected)

	// Normale Items: gedämpftes Grau, damit Orange hervorsticht
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(ThemeColorInactive)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.
		Foreground(ThemeColorInactive)


	return delegate
}

func newList(agents []*agent.Agent) list.Model {
	listItems := make([]list.Item, len(agents))
	index := 0
	for _, val := range agents {
		listItems[index] = listItem(listItem{
			botName: val.Name(),
			botDesc: val.Description(),
		})
		index++
	}
	l := list.New(listItems, newListDelegate(), listWidth, 0)
	l.Title = "Agent Chats"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)

	
	
l.Styles.Title = l.Styles.Title.
	Background(lipgloss.Color("")). // Hintergrund entfernen
	Foreground(ThemeColorBorder).
	Bold(true).
	Padding(0, 11)

	return l
}

func newTextArea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Send message..."
	ta.Focus()
	ta.CharLimit = 30000
	ta.SetWidth(50)
	ta.SetHeight(4)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	return ta
}

func initialTui(server *server.Server) *Tui {
	
	return &Tui{
		textarea: newTextArea(),
		list:      newList(server.GetAgentList()),
		focus:     focusInput,
		server:    server,
		inputChan: make(chan history.Token, 128),
		selectedAgent: nil,
	}
}

func (tui *Tui) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tui.waitForToken())
	cmds = append(cmds, textinput.Blink)
	return tea.Batch(cmds...)
}
