package platforms

import (
	"fmt"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
)

// Holds the global tool config
var cfg *config.Config = nil


// Holds all platforms
var registry []Platform = []Platform{}


type Platform interface {
	Name() string
	AgentName() string
	Listen(agent handles.AgentHandle) 
}


func Register(plat Platform) {
	for _, pl := range registry {
		if pl.Name() == plat.Name() {
			panic(fmt.Sprintf("platform %q already registered", pl.Name()))
		}
	}
	registry = append(registry, plat)
}


func All() []Platform {
    result := make([]Platform, len(registry))
    copy(result, registry)
    return result
}


func LoadPlatformConfig(path string) error {
	conf, err := config.New(path)
	if err != nil {
		return err
	}
	cfg = conf
	return nil
}


func GetPlatformConfig() *config.Config {
    if cfg == nil {
        panic("platforms: config not initialized.")
    }
    return cfg
}
