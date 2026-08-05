package agent

import (
	"fmt"
	"strings"
)


// checkCommand checks whether the given text is a valid command
// name and arguments, and handleCommand is called. Returns true if
// the text was handled as a command, false otherwise.
func (agent *Agent) CheckCommand(cmdText string) (bool, string) {
	cmdText = strings.TrimSpace(cmdText)

	if !strings.HasPrefix(cmdText, "/") {
		return false, ""
	}

	// remove the leading "/" and split into fields by whitespace
	trimmed := strings.TrimPrefix(cmdText, "/")
	fields := strings.Fields(trimmed)

	if len(fields) == 0 {
		// only "/" was entered without a command name
		return false, ""
	}

	name := strings.ToLower(fields[0])
	args := fields[1:]

	return true, agent.handleCommand(name, args)
}

// handleCommand handles the parsed command based on its name.
// args contains all arguments following the command name.
func (agent *Agent) handleCommand(name string, args []string) string {
	switch name {
	case "help":
		return agent.handleHelp(args)

	case "clear":
		return agent.handleClear(args)

	case "interrupt":
		return agent.handleInterrupt(args)

	default:
		return fmt.Sprintf("Unknown command: /%s\n", name)
	}
}


const NoArgs = "This command does not take arguments!"

func (agent *Agent) handleHelp(args []string) string {
	if len(args) > 0 {
		return NoArgs
	}

	return `## Available Commands
	 
- **/help** — Show this help message
- **/clear** — Clear the chat history of the selected agent
- **/interrupt** — Interrupt the currently running agent
	 
Type a command starting with "/" and press **Enter** to run it.`

}

func (agent *Agent) handleClear(args []string) string {
	if len(args) > 0 {
		return NoArgs
	}
	agent.Client.ClearHistory()
	return "Agent history has been reset."
}

func (agent *Agent) handleInterrupt(args []string) string {
	if len(args) > 0 {
		return NoArgs
	}
	agent.Client.Interrupt()
	return "Agent has been interrupted."
}
