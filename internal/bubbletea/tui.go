package bubbletea

import (
	"log"

 	"github.com/alex0esc/ceres/internal/server"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

// used to send messages to tui
type TokenMsg openai.Token

// focused componen saved as number
type focusState int

const (
	focusList focusState = iota
	focusInput
)


// tui hold necessary components for the ui
type Tui struct {
	viewport   viewport.Model
	textinput  textinput.Model
	list       list.Model
	ready      bool
	focus      focusState

	rendererUser   glamour.TermRenderer
	rendererAgent   glamour.TermRenderer
	rendererToolCall  glamour.TermRenderer


	server *server.Server
	selectedAgent string

	//current text
	messages []string
	tokens []TokenMsg

	inputChan chan TokenMsg
}


//runs the tui and return the tea.programm to send information and exit tui
func RunTui(server *server.Server) *tea.Program {
	program := tea.NewProgram(
		initialTui(server),
		tea.WithAltScreen(), // make it full screen
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("could not start TUI: %v+", err)
	}

	return program
}
