package platforms

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/bwmarrin/discordgo"
)

type Discord struct {
	session     *discordgo.Session
	stopChannel chan struct{}

	botToken       string
	agentName      string
	userID         string
	messageTimeout time.Duration
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

	messageTimeout, err := time.ParseDuration(config.ReadEntry(cfg, "discord.message_timeout", "60m"))
	if err != nil {
		log.Fatalf("error while parsing discord.message_timeout in server config: %v", err)
	}

	return &Discord{
		botToken:       botToken,
		agentName:      agentName,
		userID:         userID,
		messageTimeout: messageTimeout,
	}
}

// NewSession creates the discord session used for listening to DMs. It is a
// no-op if the session has already been created.
func (d *Discord) NewSession() error {
	session, err := discordgo.New("Bot " + d.botToken)
	if err != nil {
		return fmt.Errorf("discord: failed to create session: %w", err)
	}
	d.session = session
	return nil
}

func (d *Discord) Name() string {
	return "discord"
}

func (d *Discord) AgentName() string {
	return d.agentName
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

	task := handles.TaskAskSingle(handles.Prompt{Text: m.Content, Images: images}, d.messageTimeout)
	resultCh := agent.SubmitTask(task)
	result := <-resultCh
	close(stopTyping)
	if result.Err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", result.Err))
	} else {
		tools.SendChunked(s, m.ChannelID, result.Response.Filter(history.EntryTypeAssistent, history.EntryTypeToolCall).String())
	}
}

// downloadImageAttachments filters a message's attachments down to images,
// downloads each one, and returns them base64-encoded. Attachments that
// aren't images or that fail to download are skipped (logged, not fatal).
func (d *Discord) downloadImageAttachments(attachments []*discordgo.MessageAttachment) []handles.ImageInput {
	if len(attachments) == 0 {
		return nil
	}

	images := make([]handles.ImageInput, 0, len(attachments))
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

		images = append(images, handles.ImageInput{
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
