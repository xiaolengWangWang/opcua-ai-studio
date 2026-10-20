package platform

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// palette is the Studio color scheme: an industrial blue on cool grey, with
// white panels in light mode and graphite panels in dark mode.
type palette map[fyne.ThemeColorName]color.Color

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

var (
	lightPrimary = rgb(0x1565c0)
	darkPrimary  = rgb(0x4c9aff)
	lightColors  = palette{
		theme.ColorNamePrimary:             lightPrimary,
		theme.ColorNameForegroundOnPrimary: rgb(0xffffff),
		theme.ColorNameBackground:          rgb(0xf3f5f8),
		theme.ColorNameOverlayBackground:   rgb(0xffffff),
		theme.ColorNameMenuBackground:      rgb(0xffffff),
		theme.ColorNameHeaderBackground:    rgb(0xe6ecf4),
		theme.ColorNameInputBackground:     rgb(0xffffff),
		theme.ColorNameInputBorder:         rgb(0xc5cdd8),
		theme.ColorNameButton:              rgb(0xe3e8ef),
		theme.ColorNameDisabledButton:      rgb(0xedf0f4),
		theme.ColorNameForeground:          rgb(0x1c2530),
		theme.ColorNamePlaceHolder:         rgb(0x5d6878),
		theme.ColorNameDisabled:            rgb(0x4f5b6a), // hints and disabled fields: Fyne's light grey was hard to read
		theme.ColorNameSeparator:           rgb(0xd6dce4),
		theme.ColorNameHover:               alpha(lightPrimary, 0x1a),
		theme.ColorNamePressed:             alpha(lightPrimary, 0x4d),
		theme.ColorNameSelection:           alpha(lightPrimary, 0x40),
		theme.ColorNameFocus:               alpha(lightPrimary, 0xcc),
		// Fyne's default green and orange fall below 2:1 on a light background.
		theme.ColorNameSuccess: rgb(0x2e7d32),
		theme.ColorNameWarning: rgb(0xb45309),
		theme.ColorNameError:   rgb(0xc62828),
	}
	darkColors = palette{
		theme.ColorNamePrimary:             darkPrimary,
		theme.ColorNameForegroundOnPrimary: rgb(0x0b1220),
		theme.ColorNameBackground:          rgb(0x161b22),
		theme.ColorNameOverlayBackground:   rgb(0x1e252e),
		theme.ColorNameMenuBackground:      rgb(0x1e252e),
		theme.ColorNameHeaderBackground:    rgb(0x212932),
		theme.ColorNameInputBackground:     rgb(0x0f141a),
		theme.ColorNameInputBorder:         rgb(0x3a4350),
		theme.ColorNameButton:              rgb(0x262d36),
		theme.ColorNameDisabledButton:      rgb(0x1f252c),
		theme.ColorNameForeground:          rgb(0xe6edf3),
		theme.ColorNamePlaceHolder:         rgb(0x9aa5b4),
		theme.ColorNameDisabled:            rgb(0xaab4c2),
		theme.ColorNameSeparator:           rgb(0x2d3540),
		theme.ColorNameHover:               alpha(darkPrimary, 0x26),
		theme.ColorNamePressed:             alpha(darkPrimary, 0x4d),
		theme.ColorNameSelection:           alpha(darkPrimary, 0x59),
		theme.ColorNameFocus:               alpha(darkPrimary, 0xcc),
	}
)

// studioTheme applies the palette over another theme, which keeps fonts,
// icons and sizes.
type studioTheme struct{ fyne.Theme }

// NewTheme returns the Studio color scheme over base.
func NewTheme(base fyne.Theme) fyne.Theme { return studioTheme{base} }

func (t studioTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	colors := lightColors
	if variant == theme.VariantDark {
		colors = darkColors
	}
	if c, ok := colors[name]; ok {
		return c
	}
	return t.Theme.Color(name, variant)
}
