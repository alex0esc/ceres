package bubbletea

import (
	"log"

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
	}

	// tea.PasteMsg (Bracketed Paste) läuft hier automatisch mit durch
	// und wird von der fokussierten Textarea eingefügt.
	if cmd := tui.updateFocusedComponent(msg); cmd != nil {
		cmds = append(cmds, cmd)
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
	rightWidth := max(msg.Width-listWidth-2, 10)
	viewportHeight := msg.Height - footerHeight
	tui.rendererUser = tui.newRendererUser(rightWidth)
	tui.rendererAgent = tui.newRendererAgent(rightWidth)
	if !tui.ready {
		tui.applyListSelection()
		tui.viewport = viewport.New(
			viewport.WithWidth(rightWidth),
			viewport.WithHeight(viewportHeight),
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
		tui.viewport.SetWidth(rightWidth)
		tui.viewport.SetHeight(viewportHeight)
	}
	// -2 wegen Border oben/unten der Liste
	tui.list.SetSize(listWidth, msg.Height-2)
	tui.textarea.SetWidth(rightWidth - 4)
	tui.viewport.SetContent(tui.getContentString())
}

// handleChunkMsg adds a msg to the current chat
func (tui *Tui) handleTokenMsg(token history.Token) {
	switch token.Type {
	case history.TokenEndOfSequence:
		tui.mergeTokens()
	default:
		if token.Type != history.TokenTypeReasoning || tui.showReasoning {
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

		if first.Type != history.TokenTypeReasoning &&
			first.Type != history.TokenTypeAssistant {
			return first
		}

		for {
			select {
			case token := <-tui.inputChan:
				if token.Type == first.Type {
					first.Content[0] += token.Content[0]
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
