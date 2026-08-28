package bubbletea

import (
	"log"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)


// focused componen saved as number
type focusState int

const (
	focusList focusState = iota
	focusInput
)


// tui hold necessary components for the ui
type Tui struct {
	viewport   viewport.Model
	textarea   textarea.Model
	list       list.Model
	ready      bool
	focus      focusState

	rendererUser   glamour.TermRenderer
	rendererAgent  glamour.TermRenderer

	selectedAgent *agent.Agent

	//current text
	messages []string
	tokens   []history.Token

	inputChan chan history.Token
	pendingToken  *history.Token
}


//runs the tui and return the tea.programm to send information and exit tui
func RunTui() *tea.Program {
	program := tea.NewProgram(
		initialTui(),
		tea.WithAltScreen(), // make it full screen
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("could not start TUI: %v+", err)
	}

	return program
}
