package bubbletea

import (
	"context"
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/openai"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// necessary update method for the tea programm
func (tui *Tui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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
	case TokenMsg:
		cmds = append(cmds, waitForToken(tui.inputChan))
		tui.handleTokenMsg(msg)
	}
	return tui, tea.Batch(cmds...)
}

func waitForToken(sub chan TokenMsg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

// to handle global key presses
func (tui *Tui) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyTab:
		tui.toggleFocus()
	case tea.KeyEnter:
		tui.handleEnter()
	case tea.KeyUp:
		if(tui.focus == focusInput) {
			tui.viewport.ScrollUp(5)
		}
	case tea.KeyDown:
		if(tui.focus == focusInput) {
			tui.viewport.ScrollDown(5)
		}

	}
	tui.applyListSelection()
	return nil
}


// changes the focues between the two fields
func (tui *Tui) toggleFocus() {
	if tui.focus == focusInput {
		tui.focus = focusList
		tui.textinput.Blur()
	} else {
		tui.focus = focusInput
		tui.textinput.Focus()
	}
}


// handles the enter key press
func (tui *Tui) handleEnter() {
	if tui.focus == focusInput {
		tui.submitMessage()
	}
}


// submits a message to channel and to the text field
func (tui *Tui) submitMessage() {
	input := tui.textinput.Value()
	if input == "" {
		return
	}
	agnt := tui.getSelectedAgent()
	if agnt != nil {
		if !tui.checkCommand(input) {
			tui.mergeTokens()
			tui.getSelectedAgent().Client.Interrupt()
			tui.appendUserMessage(input)
			res := agnt.SubmitTask(context.Background(), input, false, 60 * time.Minute)
			go func() {
				err := (<-res).Err
				if err != nil {
					log.Printf("error while submitting message in the cli: %v", err)
				}
			}()
		}
	} else {
		tui.appendAgentMessage("No Agent selected!")
	}
	tui.viewport.SetContent(tui.getContentString())
	tui.textinput.Reset()
	tui.viewport.GotoBottom()
}


// feeds the current list element into the text field
func (tui *Tui) applyListSelection() {
	if selected, ok := tui.list.SelectedItem().(listItem); ok {
		if(tui.selectedAgent == selected.botName) {
			return
		}
		old := tui.getSelectedAgent()
		if old != nil {
			old.Client.ClearOnEvent()
		} 
		tui.selectedAgent = selected.botName
		tui.loadAgentHistory()
		tui.viewport.SetContent(tui.getContentString())
		tui.getSelectedAgent().Client.SetOnEvent(func(token openai.Token) {
			tui.inputChan <- TokenMsg(token)
		})
	}
}


// changes the size of the components accordingly
func (tui *Tui) handleWindowSizeMsg(msg tea.WindowSizeMsg) {
	rightWidth := max(msg.Width-listWidth-2, 10)
	viewportHeight := msg.Height - footerHeight
	if !tui.ready {
		tui.viewport = viewport.New(rightWidth, viewportHeight)
		tui.viewport.SetContent(tui.getContentString())
		tui.ready = true
	} else {
		tui.viewport.Width = rightWidth
		tui.viewport.Height = viewportHeight
		tui.applyListSelection()
	}
	// -2 wegen Border oben/unten der Liste
	tui.list.SetSize(listWidth, msg.Height-2)
	tui.textinput.Width = rightWidth - 4
	tui.rendererUser = tui.newRendererUser(tui.viewport.Width)
	tui.rendererAgent = tui.newRendererAgent(tui.viewport.Width)
	tui.rendererToolCall = tui.newRendererToolCall(tui.viewport.Width)
	tui.viewport.SetContent(tui.getContentString())
}


// handleChunkMsg adds a msg to the current chat
func (tui *Tui) handleTokenMsg(token TokenMsg) {
	if token.Type == openai.TokenEndOfSequence {
		tui.mergeTokens()
		tui.tokens = nil
	} else {
		tui.tokens = append(tui.tokens, token)
	}
	tui.viewport.SetContent(tui.getContentString())
	tui.viewport.GotoBottom()
}


// updates the focused components based on their librarie
func (tui *Tui) updateFocusedComponent(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch tui.focus {
	case focusList:
		tui.list, cmd = tui.list.Update(msg)
	case focusInput:
		tui.textinput, cmd = tui.textinput.Update(msg)
	}
	return cmd
}
