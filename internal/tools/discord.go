
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)

// DiscordTool bundles discord operations, dispatched via "action":
// "create", "post", "list", "remove", "dm". It creates and manages its own
// discordgo session independently of the Discord platform, using the REST
// API only (no gateway connection is opened, so no Session.Open() call is
// needed for these operations).
type DiscordTool struct {
	session *discordgo.Session
	guildID string
	userID  string

	// used to read file attachments out of the sandbox container for
	// action='post' and action='dm'
	containerName string
	timeout       time.Duration
	maxFileSize   int64
}

// NewDiscordTool constructs a DiscordTool, reading all relevant config
// values once up front and creating its own discordgo session.
func NewDiscordTool() *DiscordTool {
	cfg := platform.GetPlatformConfig()

	botToken := config.ReadEntry(cfg, "discord.bot_token", "<token>")
	guildID := config.ReadEntry(cfg, "discord.guild_id", "<guild_id>")
	userID := config.ReadEntry(cfg, "discord.user_id", "<id>")

	// reuse the sandbox tool config, same as ViewImageTool, since files are
	// read out of the same container
	toolCfg := tool.GetToolConfig()
	containerName := config.ReadEntry(toolCfg, "sandbox.container_name", "ceres-sandbox")

	timeout, err := time.ParseDuration(config.ReadEntry(toolCfg, "sandbox.timeout", "120s"))
	if err != nil {
		panic(fmt.Errorf("discord_tool: error while parsing sandbox.timeout in toolconfig.toml: %w", err))
	}
	if timeout <= 0 {
		panic(fmt.Errorf("discord_tool: sandbox.timeout must be positive"))
	}

	// Discord's default per-file upload limit for bots without boosted
	// guild perks is 25MB; default conservatively below that.
	var defSize int64 = 8192
	maxFileSize := config.ReadEntry(toolCfg, "discord.max_file_size_kb", defSize) * 1024

	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		panic(fmt.Errorf("discord_tool: failed to create session: %w", err))
	}

	return &DiscordTool{
		session:       session,
		guildID:       guildID,
		userID:        userID,
		containerName: containerName,
		timeout:       timeout,
		maxFileSize:   maxFileSize,
	}
}

func (t *DiscordTool) Name() string {
	return "discord"
}

func (t *DiscordTool) Description() string {
	return fmt.Sprintf(
		"Performs discord operations, depending on 'action'. "+
			"action='create': creates a text channel named 'channel' if it doesn't exist yet. "+
			"action='post': sends 'message' to the existing channel named 'channel'. "+
			"action='list': lists all text channels in the guild. "+
			"action='remove': deletes the existing channel named 'channel'. "+
			"action='dm': sends 'message' directly to the user. "+
			"For action='post' and action='dm', 'files' may list one or more file paths inside the "+
			"sandbox container (absolute or relative to the sandbox working directory) to attach to the "+
			"message. Either 'message' or 'files' (or both) must be provided. Attached files must each be "+
			"at most %d bytes, and the combined message content is limited to 2000 characters.",
		t.maxFileSize,
	)
}

func (t *DiscordTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"dm", "create", "post", "list", "remove"},
				"description": "Which operation to perform.",
			},
			"channel": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='create', action='post' and action='remove': the channel name " +
					"(without '#'). Ignored for action='list' and action='dm'.",
			},
			"message": map[string]any{
				"type": []string{"string", "null"},
				"description": "For action='post' and action='dm': the message content to send. May be omitted " +
					"(null) if 'files' is provided instead. Ignored for other actions.",
			},
			"files": map[string]any{
				"type": []string{"array", "null"},
				"items": map[string]any{
					"type": "string",
				},
				"description": "For action='post' and action='dm': optional list of file paths inside the " +
					"sandbox container to attach. Ignored for other actions.",
			},
		},
		"required":             []string{"action", "channel", "message", "files"},
		"additionalProperties": false,
	}
}

type discordToolArgs struct {
	Action  string   `json:"action"`
	Channel string   `json:"channel"`
	Message string   `json:"message"`
	Files   []string `json:"files"`
}

func (t *DiscordTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args discordToolArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("discord: invalid arguments: %w", err)
		}

		switch args.Action {
		case "dm":
			return t.discordDM(ctx, args.Message, args.Files)
		case "create":
			return t.discordCreateChannel(args.Channel)
		case "post":
			return t.discordPost(ctx, args.Channel, args.Message, args.Files)
		case "list":
			return t.discordListChannels()
		case "remove":
			return t.discordRemoveChannel(args.Channel)
		case "":
			return "", fmt.Errorf("discord: 'action' is required (must be 'create', 'post', 'list', 'remove' or 'dm')")
		default:
			return "", fmt.Errorf("discord: unknown action %q (must be 'create', 'post', 'list', 'remove' or 'dm')", args.Action)
		}
	}
}

// discordDM sends a direct message, optionally with file attachments, to
// the user configured under "discord.user_id".
func (t *DiscordTool) discordDM(ctx context.Context, message string, files []string) (string, error) {
	if message == "" && len(files) == 0 {
		return "", fmt.Errorf("discord_dm: either message or files must be provided")
	}

	if t.userID == "" {
		return "", fmt.Errorf("discord_dm: user_id is not configured under 'discord.user_id'")
	}

	channel, err := t.session.UserChannelCreate(t.userID)
	if err != nil {
		return "", fmt.Errorf("discord_dm: failed to create DM channel with user %s: %w", t.userID, err)
	}

	if err := t.sendMessageWithFiles(ctx, channel.ID, message, files); err != nil {
		return "", fmt.Errorf("discord_dm: %w", err)
	}

	out, err := json.Marshal(map[string]any{"channel_id": channel.ID, "user_id": t.userID})
	if err != nil {
		return "", fmt.Errorf("discord_dm: failed to marshal result: %w", err)
	}
	return string(out), nil
}

// discordCreateChannel creates a text channel with the given name if none
// exists yet.
func (t *DiscordTool) discordCreateChannel(channel string) (string, error) {
	if channel == "" {
		return "", fmt.Errorf("discord_create_channel: channel must not be empty")
	}

	existing, err := t.findChannelByName(channel)
	if err != nil {
		return "", fmt.Errorf("discord_create_channel: failed to list channels: %w", err)
	}
	if existing != nil {
		out, _ := json.Marshal(map[string]any{"channel_id": existing.ID, "created": false})
		return string(out), nil
	}

	newChannel, err := t.session.GuildChannelCreate(t.guildID, channel, discordgo.ChannelTypeGuildText)
	if err != nil {
		return "", fmt.Errorf("discord_create_channel: failed to create channel: %w", err)
	}

	out, err := json.Marshal(map[string]any{"channel_id": newChannel.ID, "created": true})
	if err != nil {
		return "", fmt.Errorf("discord_create_channel: failed to marshal result: %w", err)
	}
	return string(out), nil
}

// discordPost sends message, optionally with file attachments, to an
// existing channel identified by name.
func (t *DiscordTool) discordPost(ctx context.Context, channel, message string, files []string) (string, error) {
	if channel == "" {
		return "", fmt.Errorf("discord_post: channel must not be empty")
	}
	if message == "" && len(files) == 0 {
		return "", fmt.Errorf("discord_post: either message or files must be provided")
	}

	target, err := t.findChannelByName(channel)
	if err != nil {
		return "", fmt.Errorf("discord_post: failed to list channels: %w", err)
	}
	if target == nil {
		return "", fmt.Errorf("discord_post: channel %q does not exist, use action='create' first", channel)
	}

	if err := t.sendMessageWithFiles(ctx, target.ID, message, files); err != nil {
		return "", fmt.Errorf("discord_post: %w", err)
	}

	out, err := json.Marshal(map[string]any{"channel_id": target.ID})
	if err != nil {
		return "", fmt.Errorf("discord_post: failed to marshal result: %w", err)
	}
	return string(out), nil
}

// sendMessageWithFiles sends message to channelID. If files is non-empty,
// each path is read out of the sandbox container and attached; the message
// is sent as a single Discord message in that case, so message must fit
// within Discord's 2000 character content limit. If files is empty, the
// existing SendChunked behavior (splitting long messages) is used instead.
func (t *DiscordTool) sendMessageWithFiles(ctx context.Context, channelID, message string, filePaths []string) error {
	if len(filePaths) == 0 {
		return SendChunked(t.session, channelID, message)
	}

	if len(message) > 2000 {
		return fmt.Errorf("message is %d characters, which exceeds Discord's 2000 character limit for messages with attachments", len(message))
	}

	files, err := t.readFilesFromContainer(ctx, filePaths)
	if err != nil {
		return err
	}

	_, err = t.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: message,
		Files:   files,
	})
	if err != nil {
		return fmt.Errorf("failed to send message with attachments: %w", err)
	}
	return nil
}


// readFilesFromContainer reads each path out of the sandbox container and
// returns them as discordgo.File attachments, ready to be sent.
func (t *DiscordTool) readFilesFromContainer(ctx context.Context, paths []string) ([]*discordgo.File, error) {
	files := make([]*discordgo.File, 0, len(paths))
	for _, path := range paths {
		data, _, err := readFileFromContainer(ctx, t.containerName, t.timeout, t.maxFileSize, path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %q: %w", path, err)
		}

		files = append(files, &discordgo.File{
			Name:   filepath.Base(path),
			Reader: bytes.NewReader(data),
		})
	}
	return files, nil
}


// discordRemoveChannel deletes an existing channel identified by name.
func (t *DiscordTool) discordRemoveChannel(channel string) (string, error) {
	if channel == "" {
		return "", fmt.Errorf("discord_remove: channel must not be empty")
	}

	target, err := t.findChannelByName(channel)
	if err != nil {
		return "", fmt.Errorf("discord_remove: failed to list channels: %w", err)
	}
	if target == nil {
		return "", fmt.Errorf("discord_remove: channel %q does not exist", channel)
	}

	if _, err := t.session.ChannelDelete(target.ID); err != nil {
		return "", fmt.Errorf("discord_remove: failed to delete channel: %w", err)
	}

	out, err := json.Marshal(map[string]any{"channel_id": target.ID, "removed": true})
	if err != nil {
		return "", fmt.Errorf("discord_remove: failed to marshal result: %w", err)
	}
	return string(out), nil
}

// discordListChannels lists all text channels in the guild.
func (t *DiscordTool) discordListChannels() (string, error) {
	channels, err := t.session.GuildChannels(t.guildID)
	if err != nil {
		return "", fmt.Errorf("discord_list_channels: failed to list channels: %w", err)
	}

	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	entries := make([]entry, 0, len(channels))
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildText {
			entries = append(entries, entry{ID: ch.ID, Name: ch.Name})
		}
	}

	out, err := json.Marshal(map[string]any{"channels": entries})
	if err != nil {
		return "", fmt.Errorf("discord_list_channels: failed to marshal result: %w", err)
	}
	return string(out), nil
}

// findChannelByName returns the text channel with the given name, or nil if
// no such channel exists.
func (t *DiscordTool) findChannelByName(name string) (*discordgo.Channel, error) {
	channels, err := t.session.GuildChannels(t.guildID)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch.Name == name && ch.Type == discordgo.ChannelTypeGuildText {
			return ch, nil
		}
	}
	return nil, nil
}
