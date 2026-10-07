package bubbletea

import (
	"image/color"

	"charm.land/lipgloss/v2"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// activeTheme is the single source of truth for the whole TUI theme. It must be
// one of Glamour's style-name constants: styles.DraculaStyle,
// styles.TokyoNightStyle, styles.DarkStyle, styles.LightStyle, styles.PinkStyle,
// styles.AsciiStyle, styles.NoTTYStyle. Change it and every color below (plus
// the rendered markdown) follows automatically.
const activeTheme = styles.TokyoNightStyle

// activeStyle resolves activeTheme to its Glamour StyleConfig.
var activeStyle = resolveStyle(activeTheme)

func resolveStyle(name string) glamouransi.StyleConfig {
	if cfg, ok := styles.DefaultStyles[name]; ok && cfg != nil {
		return *cfg
	}
	return styles.DraculaStyleConfig
}

// markdownStyle returns activeStyle with the markdown heading prefixes
// ("#", "##", ...) stripped, so headings render without leading hashes.
func markdownStyle() glamouransi.StyleConfig {
	style := activeStyle
	style.H1.Prefix = ""
	style.H2.Prefix = ""
	style.H3.Prefix = ""
	style.H4.Prefix = ""
	style.H5.Prefix = ""
	style.H6.Prefix = ""
	return style
}

// colorOf resolves a glamour *string color to a lipgloss color, falling back to
// the given value when the active style does not define one.
func colorOf(c *string, fallback string) color.Color {
	if c != nil && *c != "" {
		return lipgloss.Color(*c)
	}
	return lipgloss.Color(fallback)
}

// chromaColor resolves a color from the code-block chroma palette, which lives
// behind a pointer and may be nil for some styles.
func chromaColor(pick func(chroma *glamouransi.Chroma) *string, fallback string) color.Color {
	if activeStyle.CodeBlock.Chroma != nil {
		return colorOf(pick(activeStyle.CodeBlock.Chroma), fallback)
	}
	return lipgloss.Color(fallback)
}

// Theme colors, each mapped to a color that the built-in styles reliably define
// (so switching activeTheme re-tints everything). Fallbacks match Dracula.
var (
	// Terminal background, taken from the code-block background.
	ThemeColorBackground = chromaColor(
		func(c *glamouransi.Chroma) *string { return c.Background.BackgroundColor },
		"#282A36",
	)

	ThemeColorInfoBar   = colorOf(activeStyle.Link.Color, "#8BE9FD")
	ThemeColorSelected  = colorOf(activeStyle.CodeBlock.Color, "#FFB86C")
	ThemeColorBorder    = colorOf(activeStyle.Heading.Color, "#BD93F9")
	ThemeColorInactive  = colorOf(activeStyle.HorizontalRule.Color, "#6272A4")
	ThemeColorSystem    = colorOf(activeStyle.Code.Color, "#50FA7B")
	ThemeColorUser      = colorOf(activeStyle.LinkText.Color, "#FF79C6")
	ThemeColorReasoning = colorOf(activeStyle.HorizontalRule.Color, "#6272A4")
	ThemeColorAgentInfo = colorOf(activeStyle.Enumeration.Color, "#8BE9FD")
)
