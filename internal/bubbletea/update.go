package bubbletea

import (
	"log"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/alex0esc/ceres/internal/app"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/command"
)

// necessary update method for the tea programm
func (tui *Tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd, handled := tui.handleKeyMsg(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if handled {
			return tui, tea.Batch(cmds...)
		}

	case tea.WindowSizeMsg:
		tui.handleWindowSizeMsg(msg)

	case history.Token:
		tui.handleTokenMsg(msg)
		cmds = append(cmds, tui.waitForToken())

	case tickMsg:
		tui.tickFrame++
		cmds = append(cmds, tui.tick())
	}

	// tea.PasteMsg (Bracketed Paste) läuft hier automatisch mit durch
	// und wird von der fokussierten Textarea eingefügt.
	if cmd := tui.updateFocusedComponent(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if tui.focus == focusInput {
		if cmd := tui.refreshAutocomplete(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	var cmd tea.Cmd
	tui.viewport, cmd = tui.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return tui, tea.Batch(cmds...)
}

// to handle global key presses
func (tui *Tui) handleKeyMsg(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		if tui.selectedAgent != nil {
			tui.selectedAgent.Client.ClearOnEvent()
		}
		return tea.Quit, true
	}

	// while the command popup is open the arrow keys drive it and tab accepts the
	// highlighted command instead of switching focus.
	if tui.focus == focusInput && tui.autocompleteActive {
		switch msg.String() {
		case "down":
			tui.autocomplete.CursorDown()
			return nil, true
		case "up":
			tui.autocomplete.CursorUp()
			return nil, true
		case "tab":
			tui.acceptAutocomplete()
			return nil, true
		}
	}

	switch msg.String() {
	case "tab":
		return tui.toggleFocus(), true

	case "enter":
		tui.handleEnter()
		return nil, true

	case "down":
		if tui.focus == focusInput &&
			tui.textarea.Line() == tui.textarea.LineCount()-1 {
			tui.textarea.CursorEnd()
			tui.textarea.InsertString("\n")
			return nil, true
		}
	}

	return nil, false
}

// changes the focues between the two fields
func (tui *Tui) toggleFocus() tea.Cmd {
	if tui.focus == focusInput {
		tui.focus = focusList
		tui.textarea.Blur()
		return nil
	}
	tui.focus = focusInput
	return tui.textarea.Focus() // Cmd startet das Cursor-Blinken neu
}

// handles the enter key press
func (tui *Tui) handleEnter() {
	switch tui.focus {
	case focusInput:
		tui.submitMessage()
	case focusList:
		tui.applyListSelection()
	}
}

// submits a message to channel and to the text field
func (tui *Tui) submitMessage() {
	input := tui.textarea.Value()
	if input == "" {
		return
	}

	// block sending an unknown slash command; arguments are not restricted.
	if strings.HasPrefix(strings.TrimSpace(input), "/") && !command.IsValid(input) {
		return
	}

	agnt := tui.selectedAgent
	if agnt != nil {
		cmd, cmd_text := command.CheckCommand(tui.selectedAgent, input)
		if !cmd {
			tsk := task.TaskAskSimple(input, tui.messageTimeout)
			res := agnt.SubmitTask(tsk)
			go func() {
				err := (<-res).Err
				if err != nil {
					log.Printf("error while submitting message from the cli: %v", err)
				}
			}()
		} else {
			tui.appendAgentMessage(cmd_text)
		}
	} else {
		tui.appendAgentMessage("*No Agent selected!*")
	}
	tui.viewport.SetContent(tui.getContentString())
	tui.textarea.Reset()
	tui.autocompleteTyping = false
	tui.autocompletePrefix = ""
	tui.viewport.GotoBottom()
}

// feeds the current list element into the text field
func (tui *Tui) applyListSelection() {
	if selected, ok := tui.list.SelectedItem().(listItem); ok {
		old := tui.selectedAgent
		if old != nil {
			old.Client.ClearOnEvent()
		}
		tui.selectedAgent = app.GetAgent(selected.botName)
		tui.selectedAgent.Client.SetOnEvent(func(token history.Token) {
			tui.inputChan <- token
		})
	}
	tui.loadAgentHistory()
	tui.viewport.SetContent(tui.getContentString())
}

// changes the size of the components accordingly
func (tui *Tui) handleWindowSizeMsg(msg tea.WindowSizeMsg) {
	tui.width = msg.Width
	tui.height = msg.Height

	// -2 list border, -2 outer padding (1 cell left + 1 cell right gutter)
	rightWidth := max(msg.Width-listWidth-4, 10)
	tui.rendererUser = tui.newRendererUser(rightWidth + 2)
	tui.rendererAgent = tui.newRendererAgent(rightWidth + 2)
	if !tui.ready {
		tui.applyListSelection()
		tui.viewport = viewport.New(
			viewport.WithWidth(rightWidth),
			viewport.WithHeight(max(msg.Height-footerHeight-2, 1)),
		)
		tui.viewport.SetContent(tui.getContentString())
		tui.viewport.MouseWheelDelta = 5
		tui.viewport.KeyMap.HalfPageDown.SetEnabled(false) // d
		tui.viewport.KeyMap.HalfPageUp.SetEnabled(false)   // u
		tui.viewport.KeyMap.PageDown.SetEnabled(false)     // f / pgdown / space
		tui.viewport.KeyMap.PageUp.SetEnabled(false)       // b / pgup
		tui.viewport.KeyMap.Down.SetEnabled(false)
		tui.viewport.KeyMap.Up.SetEnabled(false)
		tui.viewport.KeyMap.Left.SetEnabled(false)
		tui.viewport.KeyMap.Right.SetEnabled(false)
		tui.ready = true
	} else {
		tui.loadAgentHistory()
	}
	// -2 wegen Border oben/unten der Liste
	tui.list.SetSize(listWidth, msg.Height-2)
	tui.applyLayout()
	tui.viewport.SetContent(tui.getContentString())
}

// sizes the viewport, text area and autocomplete popup so the input box (and its
// border) grows upward by the popup height while the chat viewport shrinks to fit.
func (tui *Tui) applyLayout() {
	rightWidth := max(tui.width-listWidth-4, 10)
	inner := rightWidth - 2

	autoHeight := 0
	listHeight := 0
	if tui.autocompleteActive {
		listHeight = tui.autocompleteHeight
		autoHeight = listHeight + autoCompleteGap
	}

	// -2 blank spacer lines above/below the chat viewport
	viewportHeight := max(tui.height-(footerHeight+autoHeight)-2, 1)

	tui.viewport.SetWidth(rightWidth)
	tui.viewport.SetHeight(viewportHeight)
	tui.textarea.SetWidth(inner)
	tui.autocomplete.SetSize(inner, listHeight)
}

// rebuilds the command popup from the current text area value. It is shown only
// while typing a bare slash command (no arguments yet). The list is only rebuilt
// when the typed prefix changes, so navigating with the arrow keys is not reset
// by the periodic tick that also flows through Update.
func (tui *Tui) refreshAutocomplete() tea.Cmd {
	var cmd tea.Cmd

	value := tui.textarea.Value()
	trimmed := strings.TrimLeft(value, " \t\n")
	typing := strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, " \n\t")
	prefix := ""
	if typing {
		prefix = strings.ToLower(strings.TrimPrefix(trimmed, "/"))
	}

	if typing == tui.autocompleteTyping && prefix == tui.autocompletePrefix {
		return nil
	}
	tui.autocompleteTyping = typing
	tui.autocompletePrefix = prefix

	var items []list.Item
	if typing {
		var matched []command.Command
		pad := 0
		for _, c := range command.All() {
			if strings.HasPrefix(strings.ToLower(c.Name), prefix) {
				matched = append(matched, c)
				pad = max(pad, len(c.Name))
			}
		}
		for _, c := range matched {
			items = append(items, commandItem{cmd: c, pad: pad})
		}
	}

	active := len(items) > 0
	newHeight := 0
	if active {
		cmd = tui.autocomplete.SetItems(items)
		tui.autocomplete.Select(0)
		newHeight = min(len(items), maxAutoItems)
	}

	if active != tui.autocompleteActive || newHeight != tui.autocompleteHeight {
		tui.autocompleteActive = active
		tui.autocompleteHeight = newHeight
		tui.applyLayout()
	}
	return cmd
}

// writes the highlighted command into the text area and closes the popup.
func (tui *Tui) acceptAutocomplete() {
	if item, ok := tui.autocomplete.SelectedItem().(commandItem); ok {
		tui.textarea.SetValue("/" + item.cmd.Name + " ")
		tui.textarea.CursorEnd()
	}
	tui.autocompleteActive = false
	tui.autocompleteHeight = 0
	tui.autocompleteTyping = false
	tui.autocompletePrefix = ""
	tui.applyLayout()
}

// handleChunkMsg adds a msg to the current chat
func (tui *Tui) handleTokenMsg(token history.Token) {
	switch token.Type {
	case history.TokenTypeEndOfSequence:
		tui.mergeTokens()
	case history.TokenTypeResetChat:
		// the history was replaced (compressed/cleared); drop any in-flight
		// tokens and rebuild the view from the client's authoritative history
		tui.loadAgentHistory()
	default:
		if token.Type != history.EntryTypeReasoning || tui.showReasoning {
			tui.tokens = append(tui.tokens, token)
		}
	}
	tui.viewport.SetContent(tui.getContentString())
	tui.viewport.GotoBottom()
}

func (tui *Tui) waitForToken() tea.Cmd {
	return func() tea.Msg {
		var first history.Token
		if tui.pendingToken != nil {
			first = tui.pendingToken.Copy()
			tui.pendingToken = nil
		} else {
			first = (<-tui.inputChan).Copy()
		}

		if first.Type != history.EntryTypeReasoning &&
			first.Type != history.EntryTypeAssistant {
			return first
		}

		for {
			select {
			case token := <-tui.inputChan:
				if token.Type == first.Type {
					first.Text += token.Text
				} else {
					tui.pendingToken = &token
					return first
				}
			default:
				return first
			}
		}
	}
}

// updates the focused components based on their librarie
func (tui *Tui) updateFocusedComponent(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch tui.focus {
	case focusList:
		tui.list, cmd = tui.list.Update(msg)
	case focusInput:
		tui.textarea, cmd = tui.textarea.Update(msg)
	}
	return cmd
}
