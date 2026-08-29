package tool

// TODO import all external tools here
import "context"


// register all external tools here tool.Register(tool.NewTool())
func RegisterExternal() error {
	return registerAlpacaMcpTools(context.Background())
}
