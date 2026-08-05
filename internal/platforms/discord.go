package platforms

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)

// discordMaxMessageLength is the maximum number of characters allowed per
// Discord message. Discord's hard limit is 2000 for normal messages (up to
// 4000 in some boosted contexts); we stay safely under that.
const discordMaxMessageLength = 1900

type Discord struct {
	session *discordgo.Session
	stopChannel chan struct{}
}

// NewSession creates the discord session and registers the discord_post tool
// with it. It is a no-op if the session has already been created.
func (d *Discord) NewSession() error {
	token := config.ReadEntry(platform.GetPlatformConfig(), "discord.bot_token", "<token>")
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("discord: failed to create session: %w", err)
	}
	guildID := config.ReadEntry(platform.GetPlatformConfig(), "discord.guild_id", "<guild_id>")
	//make sure discord post has the right parameters
	tool.Get("discord_post").(*tools.DiscordPost).SetSessionGuildID(session, guildID)
	d.session = session
	return nil
}


func (d *Discord) Name() string {
	return "discord"
}


func (d *Discord) AgentName() string {
	return config.ReadEntry(platform.GetPlatformConfig(), "discord.agent_name", "Ceres")
}


// Listen blocks and listens for incoming DMs until the process is terminated.
func (d *Discord) Listen(agent handles.AgentHandle) {
	if err := d.NewSession(); err != nil {
		log.Printf("discord: failed to initialize session: %v", err)
		return
	}

	// we need DM intents + message content to be able to read DM contents
	d.session.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	d.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// run in its own goroutine so a slow agent doesn't block the gateway handler
		go d.handleMessage(s, m, agent)
	})
	if err := d.session.Open(); err != nil {
		log.Printf("discord: failed to open connection: %v", err)
		return
	}
	d.stopChannel = make(chan struct{})	
	<-d.stopChannel
	err := d.session.Close()
	if err != nil {
		log.Printf("error while closing discord session: %v", err)
	}
}

func (d *Discord) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate, agent handles.AgentHandle) {
	// ignore the bot's own messages
	if m.Author.ID == s.State.User.ID {
		return
	}
	// only handle DMs (empty GuildID = direct message, not a server message)
	if m.GuildID != "" {
		return
	}
	// only allow the configured user to talk to the bot
	allowedUserID := config.ReadEntry(platform.GetPlatformConfig(), "discord.user_id", "<id>")
	if allowedUserID == "" || m.Author.ID != allowedUserID {
		return
	}
	// show typing indicator while the task is being processed
	stopTyping := make(chan struct{})
	go func() {
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		// send it once immediately, then keep refreshing every 8s
		_ = s.ChannelTyping(m.ChannelID)
		for {
			select {
			case <-ticker.C:
				_ = s.ChannelTyping(m.ChannelID)
			case <-stopTyping:
				return
			}
		}
	}()
	if cmd, msg := command.CheckCommand(agent, m.Content); cmd {
		close(stopTyping)
		sendChunked(s, m.ChannelID, msg)
		return
	}
	resultCh := agent.SubmitTask(context.Background(), m.Content, false, 60*time.Minute)
	result := <-resultCh
	close(stopTyping)
	if result.Err != nil {
		sendChunked(s, m.ChannelID, fmt.Sprintf("Error: %v", result.Err))
		return
	}
	sendChunked(s, m.ChannelID, result.Response)
}


// StopListen signals Listen to stop blocking, causing the discord session to
// close. It is a no-op if Listen isn't currently running.
func (d *Discord) StopListen() {
	if d.stopChannel == nil {
		return
	}
	select {
	case <-d.stopChannel:
	default:
		close(d.stopChannel)
	}
}

// sendChunked sends msg to the given channel, splitting it into multiple
// messages if it exceeds Discord's length limit.
func sendChunked(s *discordgo.Session, channelID, msg string) {
	for _, chunk := range splitMessage(msg, discordMaxMessageLength) {
		if chunk == "" {
			continue
		}
		if _, err := s.ChannelMessageSend(channelID, chunk); err != nil {
			log.Printf("error while sending message to discord: %v", err)
			return
		}
	}
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
	platform.Register(&Discord{})
}
