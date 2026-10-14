//go:build windows

package platform

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

type windowsTheme struct {
	fyne.Theme
	regular, bold fyne.Resource
}

func (t windowsTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Monospace || style.Symbol {
		return t.Theme.Font(style)
	}
	if style.Bold && t.bold != nil {
		return t.bold
	}
	return t.regular
}

// Color 在浅色主题下加深状态色：Fyne 默认的绿/橙在白底上对比度不足 2:1，状态文字看不清。
func (t windowsTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if variant == theme.VariantLight {
		switch name {
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 0x2e, G: 0x7d, B: 0x32, A: 0xff}
		case theme.ColorNameWarning:
			return color.NRGBA{R: 0xb4, G: 0x53, B: 0x09, A: 0xff}
		case theme.ColorNameError:
			return color.NRGBA{R: 0xc6, G: 0x28, B: 0x28, A: 0xff}
		}
	}
	return t.Theme.Color(name, variant)
}

func (t windowsTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 17
	case theme.SizeNameCaptionText:
		return 10
	}
	return t.Theme.Size(name)
}

// ConfigureApp 从 Windows 自带的微软雅黑字体集合中选取 Microsoft YaHei UI 字体。
func ConfigureApp(app fyne.App) {
	fontDir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
	regular, err := yaheiResource(filepath.Join(fontDir, "msyh.ttc"))
	if err != nil {
		log.Printf("startup font regular: %v", err)
		return
	}
	bold, err := yaheiResource(filepath.Join(fontDir, "msyhbd.ttc"))
	if err != nil {
		log.Printf("startup font bold: %v", err)
	}
	app.Settings().SetTheme(windowsTheme{Theme: theme.DefaultTheme(), regular: regular, bold: bold})
	log.Printf("startup font: Microsoft YaHei UI")
}

func yaheiResource(path string) (fyne.Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("invalid font %s", path)
	}
	if string(data[:4]) != "ttcf" {
		return fyne.NewStaticResource(filepath.Base(path), data), nil
	}
	loaders, err := ot.NewLoaders(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var selected *ot.Loader
	for _, ld := range loaders {
		meta, _ := font.Describe(ld, nil)
		if strings.Contains(strings.ToLower(meta.Family), "yahei ui") {
			selected = ld
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("Microsoft YaHei UI face not found in %s", path)
	}
	standalone, err := standaloneFont(selected)
	if err != nil {
		return nil, err
	}
	return fyne.NewStaticResource(filepath.Base(path)+".ttf", standalone), nil
}
