package appconfig

import (
	"log"

	"github.com/alex0esc/ceres/pkg/config"
)

var cfg *config.Config

// Load reads the app config from path. It is called once at startup, before
// anything reads global settings.
func Load(path string) error {
	conf, err := config.New(path)
	if err != nil {
		return err
	}
	cfg = conf
	return nil
}

// Get returns the loaded app config. It is safe to call from tools (including
// external tools) as long as Load ran first.
func Get() *config.Config {
	if cfg == nil {
		log.Fatal("appconfig: config not initialized")
	}
	return cfg
}
