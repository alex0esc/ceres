package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)

// DiscordMsgTool sends messages to Discord channels or direct messages to the user.
// If 'channel' is omitted or empty, the message is sent as a DM to the configured user.
// If 'channel' is provided, the message is sent to that channel.
type DiscordMsgTool struct {
	session       *discordgo.Session
	guildID       string
	userID        string
	containerName string
	timeout       time.Duration
	maxFileSize   int64
}

// NewDiscordMsgTool constructs a DiscordMsgTool, reading all relevant config
// values once up front and creating its own discordgo session.
func NewDiscordMsgTool() *DiscordMsgTool {
	cfg := platform.GetPlatformConfig()

	botToken := config.ReadEntry(cfg, "discord.bot_token", "<token>")
	guildID := config.ReadEntry(cfg, "discord.guild_id", "<guild_id>")
	userID := config.ReadEntry(cfg, "discord.user_id", "<id>")

	toolCfg := tool.GetToolConfig()
	containerName := config.ReadEntry(toolCfg, "sandbox.container_name", "ceres-sandbox")
	timeout := config.ReadEntry(toolCfg, "sandbox.timeout", time.Second*120)

	var defSize int64 = 8192
	maxFileSize := config.ReadEntry(toolCfg, "discord.max_file_size_kb", defSize) * 1024

	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		slog.Error(fmt.Sprintf("discord_msg: failed to create session: %v", err))
	}

	return &DiscordMsgTool{
		session:       session,
		guildID:       guildID,
		userID:        userID,
		containerName: containerName,
		timeout:       timeout,
		maxFileSize:   maxFileSize,
	}
}

func (t *DiscordMsgTool) Name() string {
	return "discord_msg"
}

func (t *DiscordMsgTool) Description() string {
	return fmt.Sprintf("Send messages to Discord. If 'channel' is omitted or empty, the message is sent as a direct message to the user. "+
		"If 'channel' is provided, the message is sent to that channel. "+
		"You can optionally attach files from the sandbox container. "+
		"Either message or files (or both) must be provided. "+
		"Attached files must each be at most %d bytes, and the combined message content is limited to 2000 characters.",
		t.maxFileSize,
	)
}

func (t *DiscordMsgTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Channel name (without '#') to post to. If omitted or empty, sends as DM to the configured user.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message content to send. May be omitted if 'files' is provided instead.",
			},
			"files": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Optional list of file paths inside the sandbox container to attach to the message.",
			},
		},
		"additionalProperties": false,
	}
}

func (t *DiscordMsgTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		if t.session == nil {
			return "", fmt.Errorf("discord_msg: invalid discord session")
		}

		var args struct {
			Channel string   `json:"channel"`
			Message string   `json:"message"`
			Files   []string `json:"files"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("discord_msg: invalid arguments: %w", err)
		}

		if args.Message == "" && len(args.Files) == 0 {
			return "", fmt.Errorf("discord_msg: either message or files must be provided")
		}

		channel := args.Channel
		if channel == "" {
			return t.sendDM(ctx, args.Message, args.Files)
		}
		return t.sendToChannel(ctx, channel, args.Message, args.Files)
	}
}

func (t *DiscordMsgTool) sendDM(ctx context.Context, message string, files []string) (string, error) {
	if t.userID == "" {
		return "", fmt.Errorf("discord_msg: user_id is not configured under 'discord.user_id'")
	}

	channel, err := t.session.UserChannelCreate(t.userID)
	if err != nil {
		return "", fmt.Errorf("discord_msg: failed to create DM channel with user %s: %w", t.userID, err)
	}

	if err := t.sendMessageWithFiles(ctx, channel.ID, message, files); err != nil {
		return "", fmt.Errorf("discord_msg: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"status":     "sent",
		"channel_id": channel.ID,
		"user_id":    t.userID,
	})
	if err != nil {
		return "", fmt.Errorf("discord_msg: failed to marshal result: %w", err)
	}
	return string(out), nil
}

func (t *DiscordMsgTool) sendToChannel(ctx context.Context, channel, message string, files []string) (string, error) {
	target, err := t.findChannelByName(channel)
	if err != nil {
		return "", fmt.Errorf("discord_msg: failed to list channels: %w", err)
	}
	if target == nil {
		return "", fmt.Errorf("discord_msg: channel %q does not exist", channel)
	}

	if err := t.sendMessageWithFiles(ctx, target.ID, message, files); err != nil {
		return "", fmt.Errorf("discord_msg: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"status":     "sent",
		"channel_id": target.ID,
		"channel":    channel,
	})
	if err != nil {
		return "", fmt.Errorf("discord_msg: failed to marshal result: %w", err)
	}
	return string(out), nil
}

func (t *DiscordMsgTool) sendMessageWithFiles(ctx context.Context, channelID, message string, filePaths []string) error {
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

func (t *DiscordMsgTool) readFilesFromContainer(ctx context.Context, paths []string) ([]*discordgo.File, error) {
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

func (t *DiscordMsgTool) findChannelByName(name string) (*discordgo.Channel, error) {
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
