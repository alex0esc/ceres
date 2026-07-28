package bubbletea


import (
	"fmt"
	"strings"
)

// checkCommand checks whether the given text is a valid command
// name and arguments, and handleCommand is called. Returns true if
// the text was handled as a command, false otherwise.
func (tui *Tui) checkCommand(cmdText string) bool {
	cmdText = strings.TrimSpace(cmdText)

	if !strings.HasPrefix(cmdText, "/") {
		return false
	}

	// remove the leading "/" and split into fields by whitespace
	trimmed := strings.TrimPrefix(cmdText, "/")
	fields := strings.Fields(trimmed)

	if len(fields) == 0 {
		// only "/" was entered without a command name
		return false
	}

	name := strings.ToLower(fields[0])
	args := fields[1:]

	tui.handleCommand(name, args)

	return true
}

// handleCommand handles the parsed command based on its name.
// args contains all arguments following the command name.
func (tui *Tui) handleCommand(name string, args []string) {
	switch name {
	case "help":
		tui.handleHelp(args)

	case "clear":
		tui.handleClear(args)

	case "interrupt":
		tui.handleInterrupt(args)

	default:
		fmt.Printf("Unknown command: /%s\n", name)
	}
}

func (tui *Tui) handleHelp(args []string) {
	if len(args) > 0 {
		tui.appendAgentMessage("Invalid arguments for **help** command!")
	}

	tui.appendAgentMessage(`## Available Commands
	 
- **/help** — Show this help message
- **/clear** — Clear the chat history of the selected agent
- **/interrupt** — Interrupt the currently running agent
	 
Type a command starting with "/" and press **Enter** to run it.`)

}

func (tui *Tui) handleClear(args []string) {
	if len(args) > 0 {
		tui.appendAgentMessage("Invalid arguments for **clear** command!")
	}
	tui.getSelectedAgent().Client.ClearHistory()
	tui.messages = nil
}

func (tui *Tui) handleInterrupt(args []string) {
	if len(args) > 0 {
		tui.appendAgentMessage("Invalid arguments for **interrupt** command!")
	}
	tui.getSelectedAgent().Client.Interrupt();
}
