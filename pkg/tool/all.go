package tool

// TODO import all external tools here
import "context"

// HINT for tools that need a manual registration after the tool config loaded
func RegisterExternal() error {
	return registerAlpacaMcpTools(context.Background())
}
