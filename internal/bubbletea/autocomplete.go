package bubbletea

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/alex0esc/ceres/pkg/command"
)

const (
	maxAutoItems    = 6 // max commands shown in the autocomplete popup
	autoCompleteGap = 1 // blank line between the popup and the text area
)

// commandItem is a single entry in the slash-command autocomplete popup. The
// display line is pre-rendered (column-aligned) so the item itself stays trivial.
type commandItem struct {
	name  string // command name, used when the entry is accepted
	title string // pre-rendered, column-aligned display line
}

func (item commandItem) FilterValue() string { return item.name }
func (item commandItem) Title() string       { return item.title }
func (item commandItem) Description() string { return "" }

// commandPopup is the slash-command autocomplete. It has no border of its own:
// it is rendered inside the input box border, which grows upward to wrap it.
type commandPopup struct {
	list      list.Model
	active    bool
	lines     int    // list lines currently shown
	lastInput string // last text-area value that was processed
}

func newCommandPopup() commandPopup {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.ShowDescription = false

	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(ThemeColorSelected).
		BorderLeft(false).
		Padding(0, 0, 0, 0)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(ThemeColorSelected).
		BorderLeft(false).
		Padding(0, 0, 0, 0)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(ThemeColorInactive).
		Padding(0, 0, 0, 0)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.
		Foreground(ThemeColorInactive).
		Padding(0, 0, 0, 0)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)

	return commandPopup{list: l}
}

// visible reports whether the popup is currently shown.
func (p *commandPopup) visible() bool { return p.active }

// height returns the number of list lines the popup occupies.
func (p *commandPopup) height() int { return p.lines }

// setSize keeps the popup list sized to the inner width of the input box.
func (p *commandPopup) setSize(width, height int) { p.list.SetSize(width, height) }

// cursorUp and cursorDown move the highlighted entry.
func (p *commandPopup) cursorUp()   { p.list.CursorUp() }
func (p *commandPopup) cursorDown() { p.list.CursorDown() }

// reset hides the popup and forgets the last processed value.
func (p *commandPopup) reset() {
	p.active = false
	p.lines = 0
	p.lastInput = ""
}

// update recomputes the suggestions for the current text-area value. It is a
// no-op while the value is unchanged, so the periodic tick does not reset the
// highlighted entry. It reports whether the popup geometry changed (so the
// caller can re-run the layout) and any command produced by updating the list.
func (p *commandPopup) update(value string) (bool, tea.Cmd) {
	if value == p.lastInput {
		return false, nil
	}
	p.lastInput = value

	items := p.suggest(value)
	active := len(items) > 0

	var cmd tea.Cmd
	newHeight := 0
	if active {
		cmd = p.list.SetItems(items)
		p.list.Select(0)
		newHeight = min(len(items), maxAutoItems)
	}

	changed := active != p.active || newHeight != p.lines
	p.active = active
	p.lines = newHeight
	return changed, cmd
}

// suggest returns the entries for the token currently being typed. It walks the
// already-completed tokens down the command tree and offers the children of the
// reached node. No suggestions are produced once a leaf is reached (its
// remaining tokens are free-form arguments) or when a completed token does not
// match any subcommand.
func (p *commandPopup) suggest(value string) []list.Item {
	_, pathTokens, partial, ok := parseCommandInput(value)
	if !ok {
		return nil
	}

	var candidates []command.Command
	var prefix string
	top := false

	if len(pathTokens) == 0 {
		// completing a top-level command name
		if !strings.HasPrefix(partial, "/") {
			return nil
		}
		prefix = strings.ToLower(strings.TrimPrefix(partial, "/"))
		candidates = command.All()
		top = true
	} else {
		node, ok := command.Lookup(strings.TrimPrefix(pathTokens[0], "/"))
		if !ok {
			return nil
		}
		for _, t := range pathTokens[1:] {
			child, ok := node.Subcommand(t)
			if !ok {
				return nil // past the command path: free-form arguments
			}
			node = child
		}
		if len(node.Subcommands) == 0 {
			return nil // leaf: the rest are free-form arguments
		}
		prefix = strings.ToLower(partial)
		candidates = node.Subcommands
	}

	var matched []command.Command
	pad := 0
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c.Name), prefix) {
			matched = append(matched, c)
			pad = max(pad, len(c.Name))
		}
	}

	// top-level names carry a leading "/", so widen the column to keep the
	// descriptions aligned.
	width := pad
	if top {
		width++
	}

	items := make([]list.Item, 0, len(matched))
	for _, c := range matched {
		name := c.Name
		if top {
			name = "/" + name
		}
		items = append(items, commandItem{
			name:  c.Name,
			title: fmt.Sprintf("%-*s     %s", width, name, c.Description),
		})
	}
	return items
}

// accept returns the text-area value with the highlighted entry written in,
// replacing only the token being completed. If nothing is selected the value is
// returned unchanged.
func (p *commandPopup) accept(value string) string {
	name, ok := p.selectedName()
	if !ok {
		return value
	}

	leading, pathTokens, _, _ := parseCommandInput(value)

	var b strings.Builder
	b.WriteString(leading)
	if len(pathTokens) == 0 {
		b.WriteString("/")
	} else {
		b.WriteString(strings.Join(pathTokens, " "))
		b.WriteString(" ")
	}
	b.WriteString(name)
	b.WriteString(" ")
	return b.String()
}

// selectedName returns the name of the highlighted entry.
func (p *commandPopup) selectedName() (string, bool) {
	if item, ok := p.list.SelectedItem().(commandItem); ok {
		return item.name, true
	}
	return "", false
}

// parseCommandInput splits a text-area value into the leading whitespace, the
// already-completed command tokens and the token currently being completed. ok
// is false when the text is not a slash command.
func parseCommandInput(value string) (leading string, pathTokens []string, partial string, ok bool) {
	trimmed := strings.TrimLeft(value, " \t\n")
	if !strings.HasPrefix(trimmed, "/") {
		return "", nil, "", false
	}
	tokens := strings.Fields(trimmed)
	if len(tokens) == 0 {
		return "", nil, "", false
	}
	leading = value[:len(value)-len(trimmed)]

	// the last token is the one being completed unless the text ends in a space
	if last := trimmed[len(trimmed)-1]; last == ' ' || last == '\t' {
		return leading, tokens, "", true
	}
	return leading, tokens[:len(tokens)-1], tokens[len(tokens)-1], true
}
