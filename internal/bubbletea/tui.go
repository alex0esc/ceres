package bubbletea

import (
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/history"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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

	//config
	messageTimeout time.Duration
	showReasoning  bool
	msgInterrupt   bool

	//current text
	messages []string
	tokens   []history.Token

	inputChan chan history.Token
	pendingToken  *history.Token
}


//runs the tui and return the tea.programm to send information and exit tui
func RunTui() error {
	initial, err := initialTui()
	if err != nil {
		return err
	}

	// Alt-Screen und Maus werden in v2 nicht mehr hier gesetzt, sondern in View()
	program := tea.NewProgram(initial)

	_, err = program.Run()
	return err
}
