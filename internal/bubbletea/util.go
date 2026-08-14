package bubbletea

import (
	"log"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
    italic := true
    margin := uint(5)

    // Basis-Dokument
    style.Document.Margin = &margin

    // Text & Absätze
    style.Text.Italic = &italic

    // Zitate & Listen
    style.List.LevelIndent = uint(2)

    renderer, err := glamour.NewTermRenderer(
        glamour.WithStyles(style),
        glamour.WithWordWrap(width),
        glamour.WithPreservedNewLines(),
    )
    if err != nil {
        log.Fatalf("TermRenderer failed to be initialized: %v", err)
    }
    return *renderer
}


func (tui *Tui) renderSystem(text string) string {
	rendered, err := tui.rendererAgent.Render(text)
	if err != nil {
		rendered = text
	}
	cleanText := ansi.Strip(rendered)
	systemStyle := lipgloss.NewStyle().
		Foreground(ThemeColorSystem).
		Italic(true).
		Bold(true)
	return systemStyle.Render(cleanText)
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
		case history.EntryTypeSystemInfo:
			tui.appendSystemMessage(entry.Content)
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
			
		var rendered string
		var err error = nil
		if len(tui.tokens) > 0 && tui.tokens[0].Type == openai.TokenTypeSystemInfo {
			rendered = tui.renderSystem(tokenText.String())
		} else {
			rendered, err = tui.rendererAgent.Render(tokenText.String())
		}
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
	switch tui.tokens[0].Type {
	case openai.TokenTypeAssistent, openai.TokenEndOfSequence: 
		tui.appendAgentMessage(text.String())
	case openai.TokenTypeUser: 
		tui.appendUserMessage(text.String())
	case openai.TokenTypeSystemInfo:
		tui.appendSystemMessage(text.String())
	}
	tui.tokens = nil
}


func (tui *Tui) appendUserMessage(msg string) {   
    rendered, err := tui.rendererUser.Render(msg)
    if err != nil {
        rendered = msg
    }

    cleanText := ansi.Strip(rendered)

    userStyle := lipgloss.NewStyle().
        Foreground(ThemeColorUser).
        Italic(true)

    tui.messages = append(tui.messages, userStyle.Render(cleanText))
}


func (tui *Tui) appendAgentMessage(msg string) {
	rendered, err := tui.rendererAgent.Render(msg)
	if err != nil {
		rendered = msg
	}
	tui.messages = append(tui.messages, rendered)
}


func (tui *Tui) appendSystemMessage(msg string) {
	tui.messages = append(tui.messages, tui.renderSystem(msg))
}
