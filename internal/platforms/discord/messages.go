package discord

import (
	"fmt"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/bwmarrin/discordgo"
)

func (d *Discord) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
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
	// all commands go through Discord's native slash commands (interactions)
	agent, ok := d.currentAgent()
	if !ok {
		close(stopTyping)
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
