package platform

import (
	"log"

	"github.com/alex0esc/ceres/pkg/handles"
)

// Holds all platforms keyed by their name
var registry = make(map[string]Platform)

type Platform interface {
	Name() string
	AgentName() string
	Listen(agent handles.AgentHandle)
	StopListen()
}


func Register(plat Platform) {
	name := plat.Name()
	if _, exists := registry[name]; exists {
		log.Fatalf("platform %q already registered", name)
	}
	registry[name] = plat
}


func ClearRegistry() {
	registry = make(map[string]Platform)
}


func Get(name string) Platform {
	plat, exists := registry[name]
	if !exists {
		log.Fatalf("unknown platform %s", name)
	} 
	return plat
}
