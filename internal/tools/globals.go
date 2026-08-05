package tools

import (
	"fmt"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/docker/docker/client"
)

//direcotry where all skills are stored
const skillBaseDir = "skills/"

//all subagents needed for subagent tools
var subAgents map[string]handles.AgentHandle = nil

// used by all sandbox-related tools (bash, read_file, write_file).
var dockerClient *client.Client = nil



func SetSubagents(agents map[string]handles.AgentHandle) {
	subAgents = agents
}

func getSubagents() map[string]handles.AgentHandle {
	if subAgents == nil {
		panic("subagents have not been intialized!")
	}
	return subAgents
}


func InitDockerClient() error {
	active := config.ReadEntry(tool.GetToolConfig(), "sandbox.active", false)
	if !active {
		return nil
	}

	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	host := config.ReadEntry(tool.GetToolConfig(), "sandbox.docker_host", "")
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize docker client: %w", err)
	}

	dockerClient = cli
	return nil
}

func getDockerClient() *client.Client {
	if dockerClient == nil {
		panic("docker client not active or initialized!")
	}
	return dockerClient
	
}
