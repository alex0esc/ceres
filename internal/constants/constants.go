package constants

import (
	"path/filepath"
)

const AgentsFolderPath = "agents/"
const DatabaseFolderPath = "database/"
const TasksDirectoryPath = "tasks/"
const LogFilePath = "logs/log.txt"
const WakeupConfigPath = "config/wakeups.toml"
const EndpointsConfigPath = "config/endpoints.toml"
const ToolConfigPath = "config/toolconfig.toml"
const PlatformConfigPath = "config/platformconfig.toml"
const AppConfigPath = "config/appconfig.toml"

// AgentDatabaseDir is the data folder of one agent: database/<agent name>/.
// Deleting the agent's data means deleting this folder.
func AgentDatabaseDir(agentName string) string {
	return filepath.Join(DatabaseFolderPath, agentName)
}

// AgentMemoriesDBPath is the agent's private memory database (memory_sql tool).
func AgentMemoriesDBPath(agentName string) string {
	return filepath.Join(AgentDatabaseDir(agentName), "memories.db")
}

// AgentPersistentDBPath is the agent's database holding history and wakeups.
func AgentPersistentDBPath(agentName string) string {
	return filepath.Join(AgentDatabaseDir(agentName), "persistent_data.db")
}
