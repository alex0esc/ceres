package bubbletea

import (
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// necessary update method for the tea programm
func (tui *Tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Enter/Shift+Enter im Input-Fokus werden komplett selbst behandelt
	// (submit bzw. manuelles Einfügen von \n), damit die Textarea nicht
	// zusätzlich noch einen eigenen Zeilenumbruch einfügt.
	if km, ok := msg.(tea.KeyMsg); ok && tui.focus == focusInput {
		switch km.String() {
		case "enter":
			cmd := tui.handleKeyMsg(km)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return tui, tea.Batch(cmds...)
		
		case "down":
			if tui.textarea.Line() == tui.textarea.LineCount()-1 {
				tui.textarea.CursorEnd()
				tui.textarea.InsertString("\n")
				return tui, tea.Batch(cmds...)
			}
		}	

	}

	cmd := tui.updateFocusedComponent(msg)
	cmds = append(cmds, cmd)
	tui.viewport, cmd = tui.viewport.Update(msg)
	cmds = append(cmds, cmd)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := tui.handleKeyMsg(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.WindowSizeMsg:
		tui.handleWindowSizeMsg(msg)
	case history.Token:
		tui.handleTokenMsg(msg)
		cmds = append(cmds, tui.waitForToken())
	}
	return tui, tea.Batch(cmds...)
}

// to handle global key presses
func (tui *Tui) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		tui.selectedAgent.Client.ClearOnEvent()
		return tea.Quit
	case "tab":
		tui.toggleFocus()
	case "enter":
		tui.handleEnter()
	}
	tui.applyListSelection()
	return nil
}

// changes the focues between the two fields
func (tui *Tui) toggleFocus() {
	if tui.focus == focusInput {
		tui.focus = focusList
		tui.textarea.Blur()
	} else {
		tui.focus = focusInput
		tui.textarea.Focus()
	}
}

// handles the enter key press
func (tui *Tui) handleEnter() {
	switch tui.focus {
	case focusInput:
		tui.submitMessage()
	case focusList:
		tui.loadAgentHistory()
		tui.viewport.SetContent(tui.getContentString())
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
			tui.selectedAgent.Client.Interrupt()
			timeout, err := time.ParseDuration(config.ReadEntry(tui.server.GetConfig(), "tui.message_timeout", "60m"))
			if err != nil {
				log.Fatalf("error while parsing tui_timeout in server config: %v", err)
			}
			task := handles.TaskAsk(input, timeout)
			res := agnt.SubmitTask(&task)
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
		if tui.selectedAgent != nil && tui.selectedAgent.Name() == selected.botName {
			return
		}
		old := tui.selectedAgent
		if old != nil {
			old.Client.ClearOnEvent()
		}
		tui.selectedAgent = tui.server.GetAgent(selected.botName)
		tui.loadAgentHistory()
		tui.viewport.SetContent(tui.getContentString())
		tui.selectedAgent.Client.SetOnEvent(func(token history.Token) {
			tui.inputChan <- token
		})
	}
}

// changes the size of the components accordingly
func (tui *Tui) handleWindowSizeMsg(msg tea.WindowSizeMsg) {
	rightWidth := max(msg.Width-listWidth-2, 10)
	viewportHeight := msg.Height - footerHeight
	tui.rendererUser = tui.newRendererUser(rightWidth)
	tui.rendererAgent = tui.newRendererAgent(rightWidth)
	if !tui.ready {
		tui.applyListSelection()
		tui.viewport = viewport.New(rightWidth, viewportHeight)
		tui.viewport.SetContent(tui.getContentString())
		tui.viewport.MouseWheelDelta = 5
		tui.viewport.KeyMap.HalfPageDown.SetEnabled(false) // d
		tui.viewport.KeyMap.HalfPageUp.SetEnabled(false)   // u
		tui.viewport.KeyMap.PageDown.SetEnabled(false)     // f / pgdown / space
		tui.viewport.KeyMap.PageUp.SetEnabled(false)       // b / pgup
		tui.viewport.KeyMap.Down.SetEnabled(false)
		tui.viewport.KeyMap.Up.SetEnabled(false)
		tui.ready = true
	} else {
		tui.viewport.Width = rightWidth
		tui.viewport.Height = viewportHeight
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
		tui.tokens = append(tui.tokens, token)
	}
	tui.viewport.SetContent(tui.getContentString())
	tui.viewport.GotoBottom()
}




func (tui *Tui) waitForToken() tea.Cmd {
	return func() tea.Msg {
		var first history.Token		
		if tui.pendingToken != nil {
			first = *tui.pendingToken
			tui.pendingToken = nil
		} else {
			first = (<-tui.inputChan).Copy()
		}
		
		if first.Type != history.TokenTypeReasoning &&
			first.Type != history.TokenTypeAssistent {
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
