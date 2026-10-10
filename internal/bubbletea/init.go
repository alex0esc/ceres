package bubbletea

import (
	"fmt"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/app"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// item type in the selectalbe list
type listItem struct {
	botName string
	botDesc string
}

func (item listItem) FilterValue() string { return item.botName }
func (item listItem) Title() string       { return item.botName }
func (item listItem) Description() string { return item.botDesc }

// item type in the slash-command autocomplete list
type commandItem struct {
	cmd command.Command
	pad int // column width the name is padded to, so descriptions align
}

func (item commandItem) FilterValue() string { return item.cmd.Name }
func (item commandItem) Title() string {
	return fmt.Sprintf("/%-*s     %s", item.pad, item.cmd.Name, item.cmd.Description)
}
func (item commandItem) Description() string { return item.cmd.Description }

const (
	listWidth      = 40
	footerHeight   = 7 // Inputbox: border top + bottom (4 + 3)
	textareaHeight = 4 // fixed inner height of the text area
	maxAutoItems   = 6 // max commands shown in the autocomplete popup
	autoCompleteGap = 1 // blank line between the popup and the text area
)

func newListDelegate() list.ItemDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(1)
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
	Background(lipgloss.NoColor{}). //remove background
	Foreground(ThemeColorBorder).
	Bold(true).
	Padding(0, 11)

	return l
}

// the slash-command popup list. It has no border of its own: it is rendered
// inside the input box border, which grows upward to wrap it.
func newAutocompleteList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.ShowDescription = false

	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(ThemeColorSelected).
		BorderLeft(false).
		Padding(0, 0, 0, 0)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(ThemeColorSelected).
		BorderLeft(false).
		Padding(0, 0, 0, 0)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(ThemeColorInactive).
		Padding(0, 0, 0, 0)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.
		Foreground(ThemeColorInactive).
		Padding(0, 0, 0, 0)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
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
	taStyles := ta.Styles()
    taStyles.Focused.CursorLine = taStyles.Focused.CursorLine.Background(ThemeColorBackground)
    taStyles.Blurred.CursorLine = taStyles.Blurred.CursorLine.Background(ThemeColorBackground)
    ta.SetStyles(taStyles)
	return ta
}

func initialTui() (*Tui, error) {
	timeout := config.ReadEntry(app.GetAppConfig(), "tui.message_timeout", time.Minute * 60)

	return &Tui{
		textarea:     newTextArea(),
		list:         newList(app.GetAgentList()),
		autocomplete: newAutocompleteList(),
		focus:        focusInput,
		inputChan: make(chan history.Token, 1024),
		selectedAgent: nil,
		messageTimeout: timeout,
		showReasoning: config.ReadEntry(app.GetAppConfig(), "tui.show_reasoning", true),
	}, nil
}

func (tui *Tui) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tui.waitForToken())
	cmds = append(cmds, textarea.Blink)
	cmds = append(cmds, tui.tick())
	return tea.Batch(cmds...)
}

// tickMsg is a periodic signal that advances the status animations.
type tickMsg time.Time

// tick schedules the next animation frame.
func (tui *Tui) tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
