package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/simulator"
	"strings"
	"time"
)

func (d *Desktop) connectionPanel() fyne.CanvasObject {
	d.target = widget.NewEntry()
	d.target.SetPlaceHolder("opc.tcp://192.168.1.10:4840")
	connect := func() {
		cfg, err := d.config()
		if err != nil {
			d.showError(err)
			return
		}
		d.allowWrite.SetChecked(false)
		d.run(func(ctx context.Context) error { return d.controller.Connect(ctx, cfg) }, d.saveInputs)
	}
	d.target.OnSubmitted = func(string) { connect() }
	d.connectBtn = widget.NewButtonWithIcon("连接", theme.LoginIcon(), connect)
	d.disconnectBtn = widget.NewButton("断开", func() {
		d.allowWrite.SetChecked(false)
		d.run(func(context.Context) error { return d.controller.Disconnect() }, nil)
	})
	d.demoBtn = widget.NewButton("启动本机演示", func() {
		d.allowWrite.SetChecked(false)
		d.run(d.controller.StartDemo, func() {
			d.target.SetText(d.controller.Snapshot().DemoTarget)
			d.policy.SetSelected("None")
			d.mode.SetSelected("None")
			d.addresses.SetText(strings.Join(append([]string{simulator.CounterNode}, simulator.ChineseNodes...), "\n"))
			d.singleNode.SetText(simulator.CounterNode)
			d.allRoot.SetText("ns=2;i=85")
			d.browseRoot.SetText("ns=2;i=85")
			d.kind.SetSelected("Int32")
			d.count.SetText("1")
		})
	})
	d.stopDemoBtn = widget.NewButton("停止演示", func() {
		d.allowWrite.SetChecked(false)
		d.run(func(context.Context) error { return d.controller.StopDemo() }, nil)
	})
	d.discoverBtn = widget.NewButton("发现端点", func() {
		target := d.target.Text
		timeout, err := time.ParseDuration(d.timeout.Text)
		if err != nil {
			d.showError(err)
			return
		}
		var ee []opcua.Endpoint
		d.run(func(ctx context.Context) error { var e error; ee, e = opcua.Endpoints(ctx, target, timeout); return e }, func() {
			var lines []string
			for _, e := range ee {
				lines = append(lines, fmt.Sprintf("%s\n%s / %s  %v", e.URL, e.Policy, e.Mode, e.Authentication))
			}
			dialog.ShowInformation("服务器端点", strings.Join(lines, "\n\n"), d.win)
		})
	})
	d.timeout = widget.NewEntry()
	d.timeout.SetText("5s")
	d.username = widget.NewEntry()
	d.password = widget.NewPasswordEntry()
	d.cert = widget.NewEntry()
	d.key = widget.NewEntry()
	d.serverCert = widget.NewEntry()
	d.policy = widget.NewSelect([]string{"None", "Basic256Sha256", "Aes128_Sha256_RsaOaep", "Aes256_Sha256_RsaPss"}, nil)
	d.policy.SetSelected("None")
	d.mode = widget.NewSelect([]string{"None", "Sign", "SignAndEncrypt"}, nil)
	d.mode.SetSelected("None")
	security := container.NewGridWithColumns(2, widget.NewForm(widget.NewFormItem("安全策略", d.policy), widget.NewFormItem("安全模式", d.mode), widget.NewFormItem("超时", d.timeout), widget.NewFormItem("用户名", d.username), widget.NewFormItem("密码", d.password)), widget.NewForm(widget.NewFormItem("客户端证书", d.cert), widget.NewFormItem("客户端私钥", d.key), widget.NewFormItem("可信服务器证书", d.serverCert)))
	d.probeBtn = widget.NewButtonWithIcon("连接数测试", theme.ComputerIcon(), d.probeSessionsDialog)
	d.connectBtn.Importance = widget.HighImportance
	row := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Endpoint", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(d.discoverBtn, d.connectBtn, d.disconnectBtn), d.target)
	demo := widget.NewLabel("匿名演示 · 回环服务器 · Counter = 42 · 含中文点位")
	demo.Importance = widget.LowImportance
	tools := container.NewHBox(d.demoBtn, d.stopDemoBtn, demo, layout.NewSpacer(), d.probeBtn)
	return container.NewVBox(row, tools, widget.NewAccordion(widget.NewAccordionItem("安全与认证", security)))
}

// section groups related controls on a card under a bold title.
func section(title string, objects ...fyne.CanvasObject) fyne.CanvasObject {
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return widget.NewCard("", "", container.NewVBox(append([]fyne.CanvasObject{heading}, objects...)...))
}
func (d *Desktop) pointsPanel() fyne.CanvasObject {
	d.addresses = widget.NewMultiLineEntry()
	d.addresses.SetMinRowsVisible(4)
	d.addresses.SetPlaceHolder("每行一个 NodeID，例如 ns=2;s=Counter\n浏览页点击变量会追加到这里")
	d.kind = widget.NewSelect(device.Types, nil)
	d.kind.SetSelected("Auto")
	d.count = widget.NewEntry()
	d.count.SetText("1")
	d.interval = widget.NewEntry()
	d.interval.SetText("1s")
	read := func() {
		pp, err := d.points()
		if err != nil {
			d.showError(err)
			return
		}
		d.runCheck(func(ctx context.Context) error { return d.controller.Read(ctx, pp) }, d.saveInputs)
	}
	d.readBtn = widget.NewButtonWithIcon("读取清单", theme.ViewRefreshIcon(), read)
	d.pollBtn = widget.NewButton("开始轮询", func() {
		pp, err := d.points()
		if err != nil {
			d.showError(err)
			return
		}
		interval, err := time.ParseDuration(d.interval.Text)
		if err != nil {
			d.showError(err)
			return
		}
		d.run(func(ctx context.Context) error { return d.controller.StartPolling(ctx, pp, interval) }, nil)
	})
	d.stopPollBtn = widget.NewButton("停止轮询", func() { d.controller.StopPolling(); d.refresh(d.controller.Snapshot()) })
	d.writeValue = widget.NewEntry()
	d.writeValue.SetPlaceHolder("标量或 JSON 数组，例如 123、中文文本、[1,2,3]")
	d.writeValue.OnSubmitted = func(string) { d.confirmWrite() }
	d.allowWrite = widget.NewCheck("允许写入", func(bool) { d.controls() })
	d.writeBtn = widget.NewButtonWithIcon("写入并读回", theme.ConfirmIcon(), d.confirmWrite)
	d.writeBtn.Importance = widget.DangerImportance
	d.writeStatus = widget.NewLabel("")
	d.writeStatus.Wrapping = fyne.TextWrapWord
	d.singleNode = widget.NewEntry()
	d.singleNode.SetPlaceHolder("输入或选择一个 NodeID")
	d.verifyBtn = widget.NewButton("单点验证", func() {
		p, err := d.singlePoint()
		if err != nil {
			d.showError(err)
			return
		}
		d.runCheck(func(ctx context.Context) error { return d.controller.VerifyPoint(ctx, p) }, nil)
	})
	d.singleNode.OnSubmitted = func(string) { d.verifyBtn.OnTapped() }
	d.allRoot = widget.NewEntry()
	d.allRoot.SetText("i=85")
	d.fullBtn = widget.NewButtonWithIcon("全量读取", theme.DownloadIcon(), func() { d.startFullRead(d.allRoot.Text) })
	d.fullBtn.Importance = widget.HighImportance
	d.stopReadBtn = widget.NewButtonWithIcon("停止读取", theme.MediaStopIcon(), func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	})
	d.pointSummary = widget.NewRichText()
	d.pointSummary.Wrapping = fyne.TextWrapWord
	d.scanStatus = widget.NewLabel("")
	d.scanStatus.Wrapping = fyne.TextWrapWord
	d.resultFilter = widget.NewSelect(filters, func(string) {
		if d.table != nil {
			d.refreshPointRows()
		}
	})
	d.resultFilter.SetSelected("全部")
	d.exportBtn = widget.NewButtonWithIcon("导出 CSV", theme.DocumentSaveIcon(), d.exportPointsDialog)
	d.exportAttrBtn = widget.NewButtonWithIcon("导出属性表", theme.DocumentSaveIcon(), d.exportAttributesDialog)
	d.importBtn = widget.NewButtonWithIcon("导入属性表验证", theme.FolderOpenIcon(), d.importAttributesDialog)
	d.jobLabel = widget.NewLabel("")
	d.jobBar = widget.NewProgressBar()
	d.jobBox = container.NewBorder(nil, nil, d.jobLabel, widget.NewButton("取消", func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	}), d.jobBar)
	d.jobBox.Hide()
	d.table = newTextTable(func() int { return len(d.visible) }, pointHeader, d.pointCell, d.pointStyle, []float32{260, 80, 55, 150, 190, 300})
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(d.visible) {
			return
		}
		r := d.snapshot.Results[d.visible[id.Row]]
		d.singleNode.SetText(r.Address.NodeID)
		// The declared type lets an imported or BAD control point be written.
		kind := string(r.Kind())
		if kind == "" {
			kind = "Auto"
		}
		d.kind.SetSelected(kind)
		n := 1
		if r.Value.Elements != nil {
			n = len(r.Value.Elements)
			if n == 0 {
				n = 1
			}
		}
		d.count.SetText(fmt.Sprint(n))
	}
	copy := widget.NewButtonWithIcon("复制结果", theme.ContentCopyIcon(), d.copyPointRows)
	d.pollBtn.SetIcon(theme.MediaPlayIcon())
	d.stopPollBtn.SetIcon(theme.MediaPauseIcon())
	d.verifyBtn.SetIcon(theme.SearchIcon())
	options := widget.NewForm(widget.NewFormItem("类型", d.kind), widget.NewFormItem("数量", d.count), widget.NewFormItem("轮询间隔", d.interval))
	// Controls sit in a scrolling side column so the result table keeps the
	// full window height for large lists.
	controls := container.NewVBox(
		section("节点清单", d.addresses, options, container.NewGridWithColumns(3, d.readBtn, d.pollBtn, d.stopPollBtn)),
		section("单点验证", d.singleNode, d.verifyBtn),
		section("全量读取", widget.NewForm(widget.NewFormItem("父节点", d.allRoot)), container.NewGridWithColumns(2, d.fullBtn, d.stopReadBtn)),
		section("viSCADA 属性表", container.NewGridWithColumns(2, d.importBtn, d.exportAttrBtn)),
		section("写入", d.writeValue, container.NewBorder(nil, nil, d.allowWrite, nil, d.writeBtn), d.writeStatus),
	)
	toolbar := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabelWithStyle("筛选", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), d.resultFilter), container.NewHBox(d.exportBtn, copy))
	results := container.NewBorder(container.NewVBox(toolbar, d.pointSummary, d.scanStatus, d.jobBox), nil, nil, nil, d.table)
	split := container.NewHSplit(container.NewVScroll(controls), results)
	split.SetOffset(.28)
	return split
}
func (d *Desktop) browsePanel() fyne.CanvasObject {
	d.browseRoot = widget.NewEntry()
	d.browseRoot.SetText("i=85")
	browse := func() {
		node := d.browseRoot.Text
		d.run(func(ctx context.Context) error { return d.controller.Browse(ctx, node) }, nil)
	}
	d.browseRoot.OnSubmitted = func(string) { browse() }
	d.browseBtn = widget.NewButton("浏览子节点", browse)
	d.browseFullBtn = widget.NewButton("读取此节点下全部变量", func() { d.startFullRead(d.browseRoot.Text) })
	d.browseSelected = map[string]bool{}
	d.selectionLabel = widget.NewLabel("")
	d.selectAllBtn = widget.NewButton("全选", func() {
		for _, n := range d.snapshot.Nodes {
			d.selectNode(n, true)
		}
		d.selectionChanged()
	})
	d.clearSelectBtn = widget.NewButton("清除选择", func() {
		d.browseSelection, d.browseSelected = nil, map[string]bool{}
		d.selectionChanged()
	})
	d.readSelectedBtn = widget.NewButtonWithIcon("读取选中节点", theme.ViewRefreshIcon(), func() {
		roots := append([]device.Node(nil), d.browseSelection...)
		d.tabs.SelectIndex(0)
		d.runCheck(func(ctx context.Context) error { return d.controller.ReadNodes(ctx, roots) }, nil)
	})
	d.browseTable = newTextTable(func() int { return len(d.snapshot.Nodes) }, browseHeader, d.browseCell, d.browseStyle, []float32{50, 420, 320, 210})
	d.browseTable.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(d.snapshot.Nodes) {
			return
		}
		n := d.snapshot.Nodes[id.Row]
		if id.Col == 0 {
			d.selectNode(n, !d.browseSelected[n.NodeID])
			d.selectionChanged()
			d.browseTable.UnselectAll()
			return
		}
		if strings.Contains(n.Class, "Variable") {
			d.appendPoint(n.NodeID)
			d.singleNode.SetText(n.NodeID)
			d.kind.SetSelected("Auto")
			d.count.SetText("1")
			d.tabs.SelectIndex(0)
		} else {
			d.browseRoot.SetText(n.NodeID)
		}
	}
	d.browseBtn.SetIcon(theme.NavigateNextIcon())
	d.readSelectedBtn.Importance = widget.HighImportance
	hint := widget.NewLabel("点击“选择”列勾选节点，可多选或全选后一起读取：对象读取其下全部变量，变量读取自身。点击其他列：对象继续浏览，变量追加到清单。Objects 根节点为 i=85。")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	selection := container.NewBorder(nil, nil, container.NewHBox(d.selectAllBtn, d.clearSelectBtn, d.readSelectedBtn), nil, d.selectionLabel)
	top := widget.NewCard("", "", container.NewVBox(container.NewBorder(nil, nil, widget.NewLabelWithStyle("父节点", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(d.browseBtn, d.browseFullBtn), d.browseRoot), hint, selection))
	return container.NewBorder(top, nil, nil, nil, d.browseTable)
}
func (d *Desktop) diagnosticsPanel() fyne.CanvasObject {
	d.logs = widget.NewLabel("")
	d.logs.Selectable = true
	d.logs.Wrapping = fyne.TextWrapWord
	d.events = widget.NewLabel("")
	d.events.Selectable = true
	d.events.TextStyle = fyne.TextStyle{Monospace: true}
	d.events.Wrapping = fyne.TextWrapBreak
	split := container.NewVSplit(container.NewVScroll(d.events), container.NewVScroll(d.logs))
	split.SetOffset(.65)
	top := container.NewVBox(widget.NewLabel("服务级请求与响应记录（READ / WRITE / BROWSE）；SQLite 按天和 50 MB 轮转。"), container.NewHBox(widget.NewButton("复制通讯详情 JSON", d.copyEvents), widget.NewButton("复制日志", func() { d.win.Clipboard().SetContent(d.logs.Text) })))
	return container.NewBorder(top, nil, nil, nil, split)
}

// newTextTable renders cells on demand, so only the visible rows are formatted,
// under a header row that stays in view while the rows scroll.
func newTextTable(rows func() int, header []string, cell func(row, col int) string, style func(row, col int) widget.Importance, widths []float32) *widget.Table {
	t := widget.NewTable(func() (int, int) { return rows(), len(widths) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		l.Importance = style(id.Row, id.Col)
		l.SetText(cell(id.Row, id.Col))
	})
	t.ShowHeaderRow = true
	t.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 && id.Col < len(header) {
			o.(*widget.Label).SetText(header[id.Col])
		}
	}
	for i, w := range widths {
		t.SetColumnWidth(i, w)
	}
	return t
}

var browseHeader = []string{"选择", "NodeID", "显示名称", "节点类型"}

func (d *Desktop) browseStyle(row, col int) widget.Importance {
	if col == 0 && row < len(d.snapshot.Nodes) && d.browseSelected[d.snapshot.Nodes[row].NodeID] {
		return widget.HighImportance
	}
	return widget.MediumImportance
}

func (d *Desktop) browseCell(row, col int) string {
	if row >= len(d.snapshot.Nodes) {
		return ""
	}
	n := d.snapshot.Nodes[row]
	switch col {
	case 0:
		if d.browseSelected[n.NodeID] {
			return "■"
		}
		return "□"
	case 1:
		return n.NodeID
	case 2:
		return n.DisplayName
	}
	return n.Class
}

// selectNode keeps the selection in the order nodes were chosen, across folders.
func (d *Desktop) selectNode(n device.Node, on bool) {
	if on == d.browseSelected[n.NodeID] {
		return
	}
	if on {
		d.browseSelected[n.NodeID] = true
		d.browseSelection = append(d.browseSelection, n)
		return
	}
	delete(d.browseSelected, n.NodeID)
	for i, s := range d.browseSelection {
		if s.NodeID == n.NodeID {
			d.browseSelection = append(d.browseSelection[:i], d.browseSelection[i+1:]...)
			break
		}
	}
}

func (d *Desktop) selectionChanged() {
	d.selectionLabel.SetText(fmt.Sprintf("已选 %d 个节点", len(d.browseSelection)))
	d.browseTable.Refresh()
	d.controls()
}
