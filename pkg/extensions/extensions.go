package extensions

import (
	"context"

	"github.com/alex0esc/ceres/pkg/alpaca"
)

// External tools are registered here. Add an import for each external tool
// package and register its tools in Register, e.g.:
//
//	import mytool "github.com/alex0esc/ceres/pkg/mytool"
//
//	func Register() {
//		tool.Register(mytool.New())
//	}

// Register adds all external tools to the tool registry. It is called after
// the config has been loaded, so tools may read global settings during
// construction. An error aborts startup.
func Register() error {
	return alpaca.RegisterMcpTools(context.Background())
}
