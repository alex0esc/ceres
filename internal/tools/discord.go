package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)

// DiscordTool bundles discord operations, dispatched via "action":
// "create", "post", "list", "remove".
type DiscordTool struct {
	session *discordgo.Session
	guildID string
}

func (t *DiscordTool) SetSessionGuildID(session *discordgo.Session, id string) {
	t.session = session
	t.guildID = id
}

func (t *DiscordTool) Name() string {
	return "discord"
}

func (t *DiscordTool) Description() string {
	return "Performs discord operations, depending on 'action'. " +
		"action='create': creates a text channel named 'channel' if it doesn't exist yet. " +
		"action='post': sends 'message' to the existing channel named 'channel'. " +
		"action='list': lists all text channels in the guild. " +
		"action='remove': deletes the existing channel named 'channel'." +
		"action='dm': sends 'message' directly to the user."
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
					"(without '#'). Ignored for action='list'.",
			},
			"message": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Required for action='post': the message content to send. Ignored otherwise.",
			},
		},
		"required":             []string{"action", "channel", "message"},
		"additionalProperties": false,
	}
}

type discordToolArgs struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
	Message string `json:"message"`
}

func (t *DiscordTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args discordToolArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("discord: invalid arguments: %w", err)
		}

		switch args.Action {
		case "dm": 
			return t.discordDM(args.Message)
		case "create":
			return t.discordCreateChannel(args.Channel)
		case "post":
			return t.discordPost(args.Channel, args.Message)
		case "list":
			return t.discordListChannels()
		case "remove":
			return t.discordRemoveChannel(args.Channel)
		case "":
			return "", fmt.Errorf("discord: 'action' is required (must be 'create', 'post', 'list' or 'remove')")
		default:
			return "", fmt.Errorf("discord: unknown action %q (must be 'create', 'post', 'list' or 'remove')", args.Action)
		}
	}
}


// discordDM sends a direct message to the user configured under "discord.user_id".
func (t *DiscordTool) discordDM(message string) (string, error) {
	if message == "" {
		return "", fmt.Errorf("discord_dm: message must not be empty")
	}

	userID := config.ReadEntry(platform.GetPlatformConfig(), "discord.user_id", "")
	if userID == "" {
		return "", fmt.Errorf("discord_dm: user_id is not configured under 'discord.user_id'")
	}

	channel, err := t.session.UserChannelCreate(userID)
	if err != nil {
		return "", fmt.Errorf("discord_dm: failed to create DM channel with user %s: %w", userID, err)
	}

	if err := sendChunked(t.session, channel.ID, message); err != nil {
		return "", fmt.Errorf("discord_dm: failed to send direct message: %w", err)
	}

	out, err := json.Marshal(map[string]any{"channel_id": channel.ID, "user_id": userID})
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

// discordPost sends message to an existing channel identified by name.
func (t *DiscordTool) discordPost(channel, message string) (string, error) {
	if channel == "" {
		return "", fmt.Errorf("discord_post: channel must not be empty")
	}
	if message == "" {
		return "", fmt.Errorf("discord_post: message must not be empty")
	}

	target, err := t.findChannelByName(channel)
	if err != nil {
		return "", fmt.Errorf("discord_post: failed to list channels: %w", err)
	}
	if target == nil {
		return "", fmt.Errorf("discord_post: channel %q does not exist, use action='create' first", channel)
	}

	if err := sendChunked(t.session, target.ID, message); err != nil {
		return "", fmt.Errorf("discord_dm: failed to send direct message: %w", err)
	}	

	out, err := json.Marshal(map[string]any{"channel_id": target.ID})
	if err != nil {
		return "", fmt.Errorf("discord_post: failed to marshal result: %w", err)
	}
	return string(out), nil
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


// discordMaxMessageLength is the maximum number of characters allowed per
// Discord message. Discord's hard limit is 2000 for normal messages (up to
// 4000 in some boosted contexts); we stay safely under that.
const discordMaxMessageLength = 1900

// sendChunked sends msg to the given channel, splitting it into multiple
// messages if it exceeds Discord's length limit.
func sendChunked(s *discordgo.Session, channelID, msg string) error {
	for _, chunk := range splitMessage(msg, discordMaxMessageLength) {
		if chunk == "" {
			continue
		}
		if _, err := s.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("error while sending message to discord: %v", err)
		}
	}
	return nil
}



// splitMessage splits s into chunks of at most maxLen characters, preferring
// to break on newlines and, failing that, on spaces, so that words and lines
// aren't cut in the middle where possible.
func splitMessage(s string, maxLen int) []string {
	if len(s) <= maxLen {
		return []string{s}
	}

	var chunks []string
	for len(s) > maxLen {
		limit := maxLen

		// try to break on the last newline within the limit
		splitAt := -1
		if idx := lastIndexInRange(s, "\n", limit); idx > 0 {
			splitAt = idx + 1 // include the newline in the current chunk
		} else if idx := lastIndexInRange(s, " ", limit); idx > 0 {
			splitAt = idx + 1 // include the space in the current chunk
		} else {
			splitAt = limit // hard cut, no good break point found
		}

		chunks = append(chunks, s[:splitAt])
		s = s[splitAt:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}


// lastIndexInRange returns the last index of sep within s[:limit], or -1 if
// not found.
func lastIndexInRange(s, sep string, limit int) int {
	if limit > len(s) {
		limit = len(s)
	}
	return strings.LastIndex(s[:limit], sep)
}

func init() {
	tool.Register(&DiscordTool{})
}
