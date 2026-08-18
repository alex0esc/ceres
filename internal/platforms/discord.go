package platforms

import (
	"fmt"
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
)



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
	tool.Get("discord").(*tools.DiscordTool).SetSessionGuildID(session, guildID)
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
		s.ChannelMessageSend(m.ChannelID, msg)
		return
	}
	timeout, err := time.ParseDuration(config.ReadEntry(platform.GetPlatformConfig(), "discord.message_timeout", "60m"))
	if err != nil {
		log.Fatalf("error while parsing tui_timeout in server config: %v", err)
	}
	task := handles.TaskAsk(m.Content, timeout)
	resultCh := agent.SubmitTask(&task)
	result := <-resultCh
	close(stopTyping)
	if result.Err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", result.Err))
	} else {
		tools.SendChunked(s, m.ChannelID, result.Response)
	}

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


func init() {
	platform.Register(&Discord{})
}
