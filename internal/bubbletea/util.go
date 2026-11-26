package bubbletea

import (
	"log"
	"strings"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/charmbracelet/glamour"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)


func (tui *Tui) newRendererAgent(width int) glamour.TermRenderer {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		log.Fatal("TermRenderer failed to be initialized!")
	}
	return *renderer
}



func (tui *Tui) newRendererUser(width int) glamour.TermRenderer {
    style := markdownStyle()
    margin := uint(5)

    style.Document.Margin = &margin
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


func (tui *Tui) renderReasoning(text string) string {
	rendered, err := tui.rendererAgent.Render(text)
	if err != nil {
		rendered = text
	}
	cleanText := ansi.Strip(rendered)
	systemStyle := lipgloss.NewStyle().
		Foreground(ThemeColorReasoning).
		Italic(true)
	return systemStyle.Render(cleanText)
}


// loadAgentHistory resynchronizes the view by closing the current
// subscription, taking a fresh snapshot and subscribing again. This is the
// only safe way to rebuild state: it guarantees no token is lost or shown
// twice, because tokens in flight on the old channel are discarded.
func (tui *Tui) loadAgentHistory() {
	if tui.selectedAgent == nil {
		return
	}
	tui.subscribeAgent(tui.selectedAgent)
}

// subscribeAgent replaces the currently selected agent and its live token
// stream with a fresh snapshot plus subscription.
func (tui *Tui) subscribeAgent(agnt *agent.Agent) {
	tui.stopSubscription()
	tui.selectedAgent = agnt

	tui.messages = nil
	tui.tokens = nil
	tui.pendingToken = nil
	tui.drainInputChan()

	if agnt == nil {
		return
	}

	snap, sub := agnt.Client.SnapshotAndSubscribe(inference.DefaultSubscriptionBuffer)
	tui.sub = sub
	done := make(chan struct{})
	exited := make(chan struct{})
	tui.subDone = done
	tui.subExited = exited

	tui.applySnapshot(snap)
	go tui.pumpTokens(sub, done, exited)
}

// stopSubscription closes the live subscription and waits for its forwarding
// goroutine to exit, so no stale tokens leak into the next agent's view.
func (tui *Tui) stopSubscription() {
	if tui.subDone != nil {
		close(tui.subDone)
		<-tui.subExited
		tui.subDone = nil
		tui.subExited = nil
	}
	if tui.sub != nil {
		tui.sub.Close()
		tui.sub = nil
	}
}

// pumpTokens forwards tokens from the subscription into the stable input
// channel consumed by the bubbletea update loop. If the client dropped the
// subscription because the buffer overflowed, it signals a reset so the update
// loop can resubscribe from a fresh snapshot.
func (tui *Tui) pumpTokens(sub *inference.Subscription, done <-chan struct{}, exited chan<- struct{}) {
	defer close(exited)
	for {
		select {
		case token, ok := <-sub.C:
			if !ok {
				if sub.Err() != nil {
					select {
					case tui.inputChan <- history.Token{Type: history.TokenTypeResetChat}:
					case <-done:
					}
				}
				return
			}
			select {
			case tui.inputChan <- token:
			case <-done:
				return
			}
		case <-done:
			return
		}
	}
}

func (tui *Tui) drainInputChan() {
	for {
		select {
		case <-tui.inputChan:
		default:
			return
		}
	}
}

// applySnapshot rebuilds messages from the stored history and replays the
// still-streamed partial answer through the same merge logic used for live
// tokens.
func (tui *Tui) applySnapshot(snap inference.Snapshot) {
	tui.messages = nil
	tui.tokens = nil
	tui.pendingToken = nil

	for entry := range snap.History.All() {
		switch entry.Type {
		case history.EntryTypeAssistant:
			tui.appendAgentMessage(entry.String())
		case history.EntryTypeUser:
			// images live inside the user entry; render them like the stream does
			if len(entry.Images) > 0 {
				img := history.Entry{Type: history.EntryTypeImage, Images: entry.Images}
				tui.appendUserMessage(img.String())
			}
			if entry.Text != "" {
				tui.appendUserMessage(entry.Text)
			}
		case history.EntryTypeToolCall:
			tui.appendSystemMessage(entry.String())
		case history.EntryTypeReasoning:
			if tui.showReasoning {
				tui.appendReasoningMessage(entry.String())
			}
		case history.EntryTypeImage:
			tui.appendUserMessage(entry.String())
		}
	}

	for _, token := range snap.Partial {
		if token.Type == history.TokenTypeResetChat {
			continue
		}
		tui.applyToken(token)
	}
}

// applyToken folds a single live token into the current view state.
func (tui *Tui) applyToken(token history.Token) {
	switch token.Type {
	case history.TokenTypeEndOfSequence:
		tui.mergeTokens()
	default:
		if token.Type != history.EntryTypeReasoning || tui.showReasoning {
			tui.tokens = append(tui.tokens, token)
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
			tokenText.WriteString(token.String())
		}
			
		var rendered string
		var err error = nil
		switch tui.tokens[0].Type {
		case history.EntryTypeToolCall:
			rendered = tui.renderSystem(tokenText.String())
		case history.EntryTypeReasoning:
			rendered = tui.renderReasoning(tokenText.String())
		default:
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
		text.WriteString(token.String())
	}
	switch tui.tokens[0].Type {
	case history.EntryTypeAssistant, history.TokenTypeEndOfSequence:
		tui.appendAgentMessage(text.String())
	case history.EntryTypeUser:
		tui.appendUserMessage(text.String())
	case history.EntryTypeToolCall:
		tui.appendSystemMessage(text.String())
	case history.EntryTypeReasoning:
		tui.appendReasoningMessage(text.String())
	case history.EntryTypeImage:
		tui.appendUserMessage(text.String())
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
        Foreground(ThemeColorUser)

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

func (tui *Tui) appendReasoningMessage(msg string) {
	tui.messages = append(tui.messages, tui.renderReasoning(msg))
}
