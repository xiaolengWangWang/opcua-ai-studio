//go:build windows

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

// 状态文字（已连接、GOOD、写入结果、错误）在浅色背景上须达到 WCAG AA 正文对比度 4.5:1。
func TestLightStatusColorsAreReadable(t *testing.T) {
	test.NewApp()
	th := windowsTheme{Theme: theme.DefaultTheme()}
	bg := luminance(th.Color(theme.ColorNameBackground, theme.VariantLight))
	for _, name := range []fyne.ThemeColorName{theme.ColorNameSuccess, theme.ColorNameWarning, theme.ColorNameError} {
		fg := luminance(th.Color(name, theme.VariantLight))
		if ratio := (math.Max(bg, fg) + 0.05) / (math.Min(bg, fg) + 0.05); ratio < 4.5 {
			t.Errorf("%s 对比度 %.2f:1 < 4.5:1", name, ratio)
		}
	}
	if th.Color(theme.ColorNameSuccess, theme.VariantDark) != theme.DefaultTheme().Color(theme.ColorNameSuccess, theme.VariantDark) {
		t.Error("深色主题不应改动")
	}
}
