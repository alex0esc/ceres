package platforms

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/bwmarrin/discordgo"
)

type Discord struct {
	session *discordgo.Session

	stopCtx    context.Context
	stopCancel context.CancelFunc

	agentName      string
	userID         string
	messageTimeout time.Duration

	// ensures the global application commands are registered only once, even
	// across gateway reconnects (each reconnect fires a Ready event).
	registerOnce sync.Once
}

// NewDiscord constructs a Discord platform, reading all relevant config
// values once up front. It creates its own session, independent of the
// DiscordTool's session, since it needs a gateway (websocket) connection
// for listening to DMs while the tool only needs the REST API.
func NewDiscord() *Discord {
	cfg := platform.GetPlatformConfig()

	botToken := config.ReadEntry(cfg, "discord.bot_token", "<token>")
	agentName := config.ReadEntry(cfg, "discord.agent_name", "Ceres")
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

func (d *Discord) AgentName() string {
	return d.agentName
}

// Listen blocks and listens for incoming DMs until the process is terminated.
func (d *Discord) Listen(agent handles.AgentHandle) {
	if d.session == nil {
		return
	}

	// we need DM intents + message content to be able to read DM contents
	d.session.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	d.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// run in its own goroutine so a slow agent doesn't block the gateway handler
		go d.handleMessage(s, m, agent)
	})
	// register the native slash commands once the gateway reports we are ready
	d.session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		d.registerOnce.Do(func() { d.registerCommands(s) })
	})
	// handle slash-command invocations and their option autocomplete
	d.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		go d.handleInteraction(s, i, agent)
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
	if d.userID == "" || m.Author.ID != d.userID {
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

	images := d.downloadImageAttachments(m.Attachments)

	tsk := task.TaskAskSingle(task.Prompt{Text: m.Content, Images: images}, d.messageTimeout)
	resultCh := agent.SubmitTask(tsk)
	result := <-resultCh
	close(stopTyping)
	if result.Err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", result.Err))
	} else {
		filtered := result.Response.Filter(history.EntryTypeAssistant)
		tools.SendChunked(s, m.ChannelID, filtered.String())
	}
}

// commandArgs declares the argument options for commands/subcommands that take
// free-form arguments. The key is the command path without a leading slash
// ("wakeup run"). Commands not listed here take no arguments. This mapping is
// Discord-specific and kept out of the command model on purpose.
var commandArgs = map[string][]*discordgo.ApplicationCommandOption{
	"wakeup run": {
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "name",
			Description:  "Name of the wakeup to run.",
			Required:     true,
			Autocomplete: true,
		},
	},
}

// registerCommands registers all commands (and their subcommands) as global
// Discord application commands, so they show up in the native slash-command
// picker with their descriptions. Global commands are also available in DMs
// with the bot (given a mutual guild).
func (d *Discord) registerCommands(s *discordgo.Session) {
	if s.State.User == nil {
		return
	}
	appID := s.State.User.ID
	if _, err := s.ApplicationCommandBulkOverwrite(appID, "", buildApplicationCommands()); err != nil {
		log.Printf("discord: failed to register application commands: %v", err)
	}
}

// buildApplicationCommands converts the registered command tree into Discord
// application commands: subcommands become SUB_COMMAND options, and any
// declared argument options are attached to the matching subcommand.
func buildApplicationCommands() []*discordgo.ApplicationCommand {
	cmds := make([]*discordgo.ApplicationCommand, 0, 8)
	for _, c := range command.All() {
		ac := &discordgo.ApplicationCommand{
			Name:        c.Name,
			Description: c.Description,
		}
		for _, sub := range c.Subcommands {
			opt := &discordgo.ApplicationCommandOption{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        sub.Name,
				Description: sub.Description,
			}
			opt.Options = commandArgs[c.Name+" "+sub.Name]
			ac.Options = append(ac.Options, opt)
		}
		// top-level argument options (none currently, but supported)
		ac.Options = append(ac.Options, commandArgs[c.Name]...)
		cmds = append(cmds, ac)
	}
	return cmds
}

// handleInteraction processes slash-command invocations and their option
// autocomplete. Only the configured user is allowed, mirroring the DM handler.
func (d *Discord) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, agent handles.AgentHandle) {
	userID := interactionUserID(i.Interaction)
	if d.userID == "" || userID != d.userID {
		return
	}

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		text := interactionCommandText(i.ApplicationCommandData())
		if handled, msg := command.CheckCommand(agent, text); handled {
			d.respondCommand(s, i, msg)
		}

	case discordgo.InteractionApplicationCommandAutocomplete:
		choices := autocompleteChoices(i.ApplicationCommandData(), agent)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{Choices: choices},
		})
	}
}

// respondCommand answers a slash-command interaction. The interaction is first
// acknowledged (required within 3s), the resulting "thinking" placeholder is
// immediately deleted, and the actual result is always sent as regular channel
// messages via SendChunked. This way the placeholder is never visible.
func (d *Discord) respondCommand(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("discord: failed to acknowledge interaction: %v", err)
		return
	}
	if err := s.InteractionResponseDelete(i.Interaction); err != nil {
		log.Printf("discord: failed to clear interaction placeholder: %v", err)
	}
	tools.SendChunked(s, i.ChannelID, msg)
}

// interactionUserID returns the id of the user who triggered the interaction,
// which is filled in differently for DMs (User) and guilds (Member.User).
func interactionUserID(i *discordgo.Interaction) string {
	if i.User != nil {
		return i.User.ID
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	return ""
}

// interactionCommandText rebuilds the textual slash command from an interaction
// so it can be fed straight into command.CheckCommand, keeping behavior and
// error messages identical to the TUI and plain-text path.
func interactionCommandText(data discordgo.ApplicationCommandInteractionData) string {
	parts := []string{"/" + data.Name}
	for _, opt := range data.Options {
		if opt.Type == discordgo.ApplicationCommandOptionSubCommand {
			parts = append(parts, opt.Name)
			for _, arg := range opt.Options {
				parts = append(parts, quoteArg(stringOptionValue(arg)))
			}
			continue
		}
		parts = append(parts, quoteArg(stringOptionValue(opt)))
	}
	return strings.Join(parts, " ")
}

// stringOptionValue extracts a string option value, returning "" for others.
func stringOptionValue(opt *discordgo.ApplicationCommandInteractionDataOption) string {
	if s, ok := opt.Value.(string); ok {
		return s
	}
	return ""
}

// quoteArg wraps a value in double quotes when it contains whitespace, so it
// survives the command argument splitter as a single argument.
func quoteArg(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, " \t\n\"") {
		return `"` + strings.ReplaceAll(v, `"`, "") + `"`
	}
	return v
}

// autocompleteChoices returns the wakeup names matching the currently focused
// option, so /wakeup run offers them as choices.
func autocompleteChoices(data discordgo.ApplicationCommandInteractionData, agent handles.AgentHandle) []*discordgo.ApplicationCommandOptionChoice {
	current := strings.ToLower(focusedOptionValue(data))

	mgr := agent.WakeupManager()
	if mgr == nil {
		return nil
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	add := func(name string) {
		if len(choices) >= 25 {
			return
		}
		if current == "" || strings.Contains(strings.ToLower(name), current) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: name})
		}
	}
	for _, w := range mgr.ListInternalWakeups() {
		add(w.Name())
	}
	for _, w := range mgr.ListExternalWakeups() {
		add(w.Name())
	}
	return choices
}

// focusedOptionValue returns the value of the option the user is currently
// typing, searching through subcommand nesting.
func focusedOptionValue(data discordgo.ApplicationCommandInteractionData) string {
	for _, opt := range data.Options {
		if opt.Focused {
			return stringOptionValue(opt)
		}
		for _, sub := range opt.Options {
			if sub.Focused {
				return stringOptionValue(sub)
			}
		}
	}
	return ""
}

// downloadImageAttachments filters a message's attachments down to images,
// downloads each one, and returns them base64-encoded. Attachments that
// aren't images or that fail to download are skipped (logged, not fatal).
func (d *Discord) downloadImageAttachments(attachments []*discordgo.MessageAttachment) []task.ImageInput {
	if len(attachments) == 0 {
		return nil
	}

	images := make([]task.ImageInput, 0, len(attachments))
	for _, att := range attachments {
		if att == nil || att.URL == "" {
			continue
		}

		data, err := downloadAttachment(att.URL)
		if err != nil {
			log.Printf("discord: failed to download attachment %q: %v", att.Filename, err)
			continue
		}

		mimeType := tools.DetectImageMimeType(att.Filename, data)
		if mimeType == "" {
			// not an image, skip
			continue
		}

		images = append(images, task.ImageInput{
			Base64Image: base64.StdEncoding.EncodeToString(data),
			MimeType:    mimeType,
		})
	}

	if len(images) == 0 {
		return nil
	}
	return images
}

// downloadAttachment fetches the raw bytes of a Discord attachment URL.
func downloadAttachment(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	return data, nil
}

func (d *Discord) StopListen() {
	d.stopCancel()
}
