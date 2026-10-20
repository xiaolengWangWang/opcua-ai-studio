package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/platform"
)

// buildMenu gathers the less frequent actions by category, so the window
// keeps its space for the data. An item that mirrors a button takes the
// button's action and enabled state.
func (d *Desktop) buildMenu() {
	d.menuButtons = map[*fyne.MenuItem]*widget.Button{}
	button := func(label string, b *widget.Button) *fyne.MenuItem {
		mi := fyne.NewMenuItem(label, func() {
			if !b.Disabled() && b.OnTapped != nil {
				b.OnTapped()
			}
		})
		d.menuButtons[mi] = b
		return mi
	}
	action := func(label string, f func()) *fyne.MenuItem { return fyne.NewMenuItem(label, f) }
	quit := action("退出", d.quit)
	quit.IsQuit = true
	file := fyne.NewMenu("文件",
		action("新建窗口", d.newWindow),
		fyne.NewMenuItemSeparator(),
		button("导入 viSCADA 属性表验证…", d.importBtn),
		button("导出 CSV…", d.exportCSVBtn),
		button("导出 viSCADA 属性表…", d.exportAttrBtn),
		action("复制结果", d.copyPointRows),
		fyne.NewMenuItemSeparator(),
		action("打开记录目录", d.openRecords),
		action("历史日志…", d.logHistoryDialog),
		fyne.NewMenuItemSeparator(),
		quit,
	)
	d.menuAutoReconnect = action("断线自动重连", func() { d.autoReconnect.SetChecked(!d.autoReconnect.Checked) })
	d.menuAutoFullRead = action("连接后自动全量读取", func() { d.autoFullRead.SetChecked(!d.autoFullRead.Checked) })
	connect := fyne.NewMenu("连接",
		button("连接", d.connectBtn),
		button("断开", d.disconnectBtn),
		button("搜索服务器…", d.scanBtn),
		button("发现端点…", d.discoverBtn),
		action("连接设置（安全与认证）…", func() { d.showSecurity(true) }),
		fyne.NewMenuItemSeparator(),
		button("保存连接配置…", d.saveProfileBtn),
		button("删除当前连接配置", d.deleteProfileBtn),
		action("连接记录…", d.connectionHistoryDialog),
		fyne.NewMenuItemSeparator(),
		d.menuAutoReconnect,
		d.menuAutoFullRead,
		button("服务器信息…", d.serverInfoBtn),
		button("连接数测试…", d.probeBtn),
		fyne.NewMenuItemSeparator(),
		button("启动本机演示", d.demoBtn),
		button("停止本机演示", d.stopDemoBtn),
	)
	d.menuAutoRefresh = action("自动刷新结果", func() { d.autoRefresh.SetChecked(!d.autoRefresh.Checked) })
	read := fyne.NewMenu("读取",
		button("单点验证", d.verifyBtn),
		button("写入选中点位…", d.writeSelectedBtn),
		fyne.NewMenuItemSeparator(),
		button("读取节点清单", d.readBtn),
		button("开始轮询", d.pollBtn),
		button("停止轮询", d.stopPollBtn),
		fyne.NewMenuItemSeparator(),
		button("全量读取", d.fullBtn),
		button("停止读取", d.stopReadBtn),
		fyne.NewMenuItemSeparator(),
		d.menuAutoRefresh,
	)
	var views []*fyne.MenuItem
	for _, mode := range viewModes {
		mi := action(mode+"视图", func() { d.viewMode.SetSelected(mode) })
		d.menuViews = append(d.menuViews, mi)
		views = append(views, mi)
	}
	d.menuSidebar = action("显示左侧操作栏", d.toggleSidebar)
	tabs := []string{"点位与读写", "节点浏览", "通讯与日志", "AI 诊断"}
	views = append(views, fyne.NewMenuItemSeparator(), d.menuSidebar,
		action("展开全部文件夹", func() { d.viewMode.SetSelected("树状"); d.tree.OpenAllBranches() }),
		action("折叠全部文件夹", func() { d.tree.CloseAllBranches() }),
		fyne.NewMenuItemSeparator())
	for i, name := range tabs {
		views = append(views, action(name, func() { d.tabs.SelectIndex(i) }))
	}
	help := fyne.NewMenu("帮助",
		action("操作手册", d.showPointHelp),
		action("质量与统计说明", d.showQualityHelp),
		button("检查更新", d.checkUpdateBtn),
		action("关于 OPC UA AI Studio", d.showAbout),
	)
	d.mainMenu = fyne.NewMainMenu(file, connect, read, fyne.NewMenu("视图", views...), help)
	d.win.SetMainMenu(d.mainMenu)
}

// updateMenu mirrors the buttons' states and the toggles into the menu,
// refreshing it only when something changed.
func (d *Desktop) updateMenu() {
	if d.mainMenu == nil {
		return
	}
	var state strings.Builder
	for mi, b := range d.menuButtons {
		mi.Disabled = b.Disabled()
	}
	d.menuAutoReconnect.Checked = d.autoReconnect.Checked
	d.menuAutoFullRead.Checked = d.autoFullRead.Checked
	d.menuAutoRefresh.Checked = d.autoRefresh.Checked
	d.menuAutoRefresh.Disabled = d.autoRefresh.Disabled()
	for i, mi := range d.menuViews {
		mi.Checked = d.viewMode.Selected == viewModes[i]
	}
	d.menuSidebar.Checked = d.split.Offset > 0.02
	for _, m := range d.mainMenu.Items {
		for _, mi := range m.Items {
			fmt.Fprintf(&state, "%t%t", mi.Disabled, mi.Checked)
		}
	}
	if s := state.String(); s != d.menuState {
		d.menuState = s
		d.mainMenu.Refresh()
	}
}

// newWindow starts another copy of the program, so several servers can be
// watched at once; each copy has its own connection, results and settings
// dialog, and all share the record archive and saved connections.
func (d *Desktop) newWindow() {
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe)
		if err = cmd.Start(); err == nil {
			cmd.Process.Release()
		}
	}
	if err != nil {
		d.showError(fmt.Errorf("无法打开新窗口：%w", err))
	}
}

func (d *Desktop) openRecords() {
	if err := platform.OpenFile(d.controller.Snapshot().RecordDir); err != nil {
		d.showError(err)
	}
}

func (d *Desktop) showAbout() {
	text := fmt.Sprintf("OPC UA AI Studio v%s\nOPC UA 桌面调试工具：搜索、连接、读取、验证与写入 OPC UA 点位。\n\n通讯记录目录：%s", d.version, d.controller.Snapshot().RecordDir)
	dialog.ShowInformation("关于", text, d.win)
}

// toggleSidebar folds the side column away or restores its width.
func (d *Desktop) toggleSidebar() {
	if d.split.Offset > 0.02 {
		d.splitOffset = d.split.Offset
		d.split.SetOffset(0)
		d.foldBtn.SetIcon(theme.NavigateNextIcon())
	} else {
		d.split.SetOffset(d.splitOffset)
		d.foldBtn.SetIcon(theme.NavigateBackIcon())
	}
	d.updateMenu()
}

// settingsDialog holds the connection settings that most connections leave
// alone: security, account, certificates and the automatic actions.
func (d *Desktop) settingsDialog(security fyne.CanvasObject) dialog.Dialog {
	discover := widget.NewButtonWithIcon("发现端点，点选填入安全策略", theme.ListIcon(), func() { d.discoverBtn.OnTapped() })
	options := container.NewVBox(d.autoReconnect, d.autoFullRead, widget.NewLabelWithStyle("自动全量读取的范围为“全量读取”中的父节点，如 i=85 或 ns=2。", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	d.securityLabel = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content := container.NewVBox(d.securityLabel, security, container.NewHBox(discover), widget.NewSeparator(), options)
	box := dialog.NewCustom("连接设置", "关闭", content, d.win)
	box.Resize(fyne.NewSize(780, 0))
	return box
}
