//go:build !windows

package platform

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Setup 在 macOS 和 Linux 上不需要做什么：有控制台或系统日志，OpenGL 也都有。
func Setup() {}

func WorkArea() (int, int)      { return 0, 0 }
func ConfigureApp(app fyne.App) { app.Settings().SetTheme(NewTheme(theme.DefaultTheme())) }
