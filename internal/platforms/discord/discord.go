package discord

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/alex0esc/ceres/pkg/appconfig"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/bwmarrin/discordgo"
)

type Discord struct {
	session *discordgo.Session

	stopCtx    context.Context
	stopCancel context.CancelFunc

	agentName      string
	userID         string
	messageTimeout time.Duration

	// agents are the agents this platform may choose from; selectedAgentName
	// is the agent currently chosen for this Discord DM.
	agents            map[string]handles.AgentHandle
	selectedMutex     sync.Mutex
	selectedAgentName string

	// ensures the global application commands are registered only once, even
	// across gateway reconnects (each reconnect fires a Ready event).
	registerOnce sync.Once
}

// NewDiscord constructs a Discord platform, reading all relevant config
// values once up front. It creates its own session, independent of the
// DiscordTool's session, since it needs a gateway (websocket) connection
// for listening to DMs while the tool only needs the REST API.
func NewDiscord() *Discord {
	cfg := appconfig.Get()

	botToken := config.ReadEntry(cfg, "discord.bot_token", "<token>")
	agentName := config.ReadEntry(cfg, "discord.default_agent", "Ceres")
	userID := config.ReadEntry(cfg, "discord.user_id", "<id>")

	messageTimeout := config.ReadEntry(cfg, "discord.message_timeout", time.Minute*60)

	stopCtx, stopCancel := context.WithCancel(context.Background())

	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		slog.Error(fmt.Sprintf("discord_platform: failed to create session (not listening): %v", err))
	}

	return &Discord{
		session:        session,
		stopCtx:        stopCtx,
		stopCancel:     stopCancel,
		agentName:      agentName,
		userID:         userID,
		messageTimeout: messageTimeout,
	}
}

func (d *Discord) Name() string {
	return "discord"
}

// Listen blocks and listens for incoming DMs until the process is terminated.
func (d *Discord) Listen(agents map[string]handles.AgentHandle) {
	if d.session == nil {
		return
	}

	if _, ok := agents[d.agentName]; !ok {
		log.Printf("discord: unknown agent %q, not listening", d.agentName)
		return
	}

	d.agents = agents

	// we need DM intents + message content to be able to read DM contents
	d.session.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	d.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// run in its own goroutine so a slow agent doesn't block the gateway handler
		go d.handleMessage(s, m)
	})
	// register the native slash commands once the gateway reports we are ready
	d.session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		d.registerOnce.Do(func() { d.registerCommands(s) })
	})
	// handle slash-command invocations and their option autocomplete
	d.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		go d.handleInteraction(s, i)
	})
	if err := d.session.Open(); err != nil {
		log.Printf("discord: failed to open connection: %v", err)
		return
	}

	<-d.stopCtx.Done()
	err := d.session.Close()
	if err != nil {
		log.Printf("error while closing discord session: %v", err)
	}
}

func (d *Discord) StopListen() {
	d.stopCancel()
}
