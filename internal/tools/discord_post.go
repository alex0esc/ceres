package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)

// DiscordPost posts a message to a discord channel, creating the channel if it doesn't exist yet.
type DiscordPost struct {
	session *discordgo.Session
	guildID string
}


func (post *DiscordPost) SetSessionGuildID(session *discordgo.Session, id string) {
	post.session = session
	post.guildID = id
}


func (t *DiscordPost) Name() string {
	return "discord_post"
}

func (t *DiscordPost) Description() string {
	return "Sends a message to a discord channel identified by its name. If the channel does not exist yet, it will be created automatically before the message is sent."
}

func (t *DiscordPost) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "The name of the discord channel to post the message to (without '#').",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The message content to send to the channel.",
			},
		},
		"required": []string{"channel", "message"},
	}
}

type discordPostArgs struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

type discordPostResult struct {
	ChannelID string `json:"channel_id"`
	Created   bool   `json:"created"`
}

func (t *DiscordPost) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args discordPostArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}

		if args.Channel == "" {
			return "", fmt.Errorf("channel must not be empty")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message must not be empty")
		}

		channelID, created, err := t.getOrCreateChannel(args.Channel)
		if err != nil {
			return "", fmt.Errorf("failed to get or create channel: %w", err)
		}

		if _, err := t.session.ChannelMessageSend(channelID, args.Message); err != nil {
			return "", fmt.Errorf("failed to send message to discord: %w", err)
		}

		result := discordPostResult{
			ChannelID: channelID,
			Created:   created,
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}

		return string(out), nil
	}
}

// getOrCreateChannel returns the channel ID for the given channel name within
// the tool's guild, creating a new text channel if none exists yet.
func (t *DiscordPost) getOrCreateChannel(name string) (channelID string, created bool, err error) {
	channels, err := t.session.GuildChannels(t.guildID)
	if err != nil {
		return "", false, err
	}

	for _, ch := range channels {
		if ch.Name == name && ch.Type == discordgo.ChannelTypeGuildText {
			return ch.ID, false, nil
		}
	}

	newChannel, err := t.session.GuildChannelCreate(t.guildID, name, discordgo.ChannelTypeGuildText)
	if err != nil {
		return "", false, err
	}

	return newChannel.ID, true, nil
}

func init() {
	tool.Register(&DiscordPost{})
}
