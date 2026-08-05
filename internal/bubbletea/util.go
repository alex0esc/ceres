package bubbletea

import (
	"log"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)


func (tui *Tui) newRendererAgent(width int) glamour.TermRenderer {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(styles.DraculaStyleConfig),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		log.Fatal("TermRenderer failed to be initialized!")
	}
	return *renderer
}


func (tui *Tui) newRendererUser(width int) glamour.TermRenderer {
	style := styles.DraculaStyleConfig

	bold := true
    color := string(ThemeColorUser)
	

	/*
	prefixStyle := lipgloss.NewStyle().
		Foreground(ThemeColorUser).
		Bold(true)
		*/

	style.Document.Prefix = "❯❯ "
	style.Document.Color = &color
	style.Text.Color = &color
	style.Text.Bold = &bold
	style.Text.Italic = &bold

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		log.Fatal("TermRenderer failed to be initialized!")
	}
	return *renderer
}


func (tui *Tui) newRendererToolCall(width int) glamour.TermRenderer {
	style := styles.DraculaStyleConfig

	color := string(ThemeColorBorder)
	
	style.Text.Color = &color

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		log.Fatal("TermRenderer failed to be initialized!")
	}
	return *renderer
}


func (tui *Tui) loadAgentHistory() {
	if tui.selectedAgent == nil {
		return
	}
	tui.messages = nil
	tui.tokens = nil
	histroy := tui.selectedAgent.Client.GetHistory()
	for entry := range histroy.All() {
		switch entry.Type {
		case history.EntryTypeAssistent:			
			tui.appendAgentMessage(entry.Content)
		case history.EntryTypeUser:
			tui.appendUserMessage(entry.Content)
		case history.EntryTypeToolCall:
			tui.appendToolCall(entry.Content)
		}
		
	}
}

func (tui *Tui) getContentString() string {
	if(tui.messages == nil) {
		return ""
	}

	var text strings.Builder
	for _, msg := range tui.messages {
		text.WriteString(msg)		
	}

	if len(tui.tokens) > 0 {
		var tokenText strings.Builder
		for _, token := range tui.tokens {
			tokenText.WriteString(token.Content)
		}

		rendered, err := tui.rendererAgent.Render(tokenText.String())
		if err != nil {
			rendered = tokenText.String()
		}	
		text.WriteString(rendered)
	}
	return text.String()
}


func (tui *Tui) mergeTokens() {
	if len(tui.tokens) <= 0 {
		return
	}
	var text strings.Builder
	for _, token := range tui.tokens {
		text.WriteString(token.Content)
	}
	if tui.tokens[0].Type == openai.TokenTypeToolCall {
		tui.appendToolCall(text.String())
	} else {
		tui.appendAgentMessage(text.String())
	}
	tui.tokens = nil
}

func (tui *Tui) appendUserMessage(msg string) {	
	rendered, err := tui.rendererUser.Render(msg)
	if err != nil {
		rendered = msg
	}
	tui.messages = append(tui.messages, rendered)
}


func (tui *Tui) appendAgentMessage(msg string) {
	rendered, err := tui.rendererAgent.Render(msg)
	if err != nil {
		rendered = msg
	}
	tui.messages = append(tui.messages, rendered)
}


func (tui *Tui) appendToolCall(msg string) {
	rendered, err := tui.rendererToolCall.Render(msg)
	if err != nil {
		rendered = msg
	}
	tui.messages = append(tui.messages, rendered)
}

