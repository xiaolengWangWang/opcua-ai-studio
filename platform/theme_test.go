package platform

import (
	"image/color"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// luminance 是 WCAG 2 的相对亮度。
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	channel := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

func contrast(a, b color.Color) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

// 正文、主色文字、主色按钮文字和状态文字（已连接、GOOD、写入结果、错误）在两种
// 配色下都须达到 WCAG AA 正文对比度 4.5:1。
func TestPaletteTextIsReadable(t *testing.T) {
	test.NewApp()
	th := NewTheme(theme.DefaultTheme())
	pairs := []struct{ fg, bg fyne.ThemeColorName }{
		{theme.ColorNameForeground, theme.ColorNameBackground},
		{theme.ColorNameForeground, theme.ColorNameOverlayBackground},
		{theme.ColorNameForeground, theme.ColorNameInputBackground},
		{theme.ColorNamePrimary, theme.ColorNameBackground},
		{theme.ColorNameForegroundOnPrimary, theme.ColorNamePrimary},
		{theme.ColorNameSuccess, theme.ColorNameBackground},
		{theme.ColorNameWarning, theme.ColorNameBackground},
		{theme.ColorNameError, theme.ColorNameBackground},
	}
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		for _, p := range pairs {
			if r := contrast(th.Color(p.fg, variant), th.Color(p.bg, variant)); r < 4.5 {
				t.Errorf("variant %d: %s on %s 对比度 %.2f:1 < 4.5:1", variant, p.fg, p.bg, r)
			}
		}
	}
}
