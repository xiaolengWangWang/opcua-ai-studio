package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
			d.addresses.SetText(simulator.CounterNode)
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
	d.probeBtn = widget.NewButton("连接数测试", d.probeSessionsDialog)
	row := container.NewBorder(nil, nil, widget.NewLabel("Endpoint"), container.NewHBox(d.discoverBtn, d.probeBtn, d.connectBtn, d.disconnectBtn), d.target)
	return container.NewVBox(row, container.NewHBox(d.demoBtn, d.stopDemoBtn, widget.NewLabel("匿名演示 · 回环服务器 · Counter = 42")), widget.NewAccordion(widget.NewAccordionItem("安全与认证", security)), widget.NewSeparator())
}
func (d *Desktop) pointsPanel() fyne.CanvasObject {
	d.addresses = widget.NewMultiLineEntry()
	d.addresses.SetMinRowsVisible(2)
	d.addresses.SetPlaceHolder("每行一个 NodeID，例如 ns=2;s=Counter")
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
	d.fullBtn = widget.NewButton("全量读取", func() { d.startFullRead(d.allRoot.Text) })
	d.stopReadBtn = widget.NewButton("停止读取", func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	})
	d.pointSummary = widget.NewLabel("")
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
	d.table = newTextTable(func() int { return len(d.visible) + 1 }, d.pointCell, []float32{260, 85, 55, 160, 195, 270})
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 || id.Row > len(d.visible) {
			return
		}
		r := d.snapshot.Results[d.visible[id.Row-1]]
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
	copy := widget.NewButton("复制结果", d.copyPointRows)
	options := container.NewGridWithColumns(3, widget.NewForm(widget.NewFormItem("类型", d.kind)), widget.NewForm(widget.NewFormItem("数量", d.count)), widget.NewForm(widget.NewFormItem("轮询间隔", d.interval)))
	single := container.NewBorder(nil, nil, widget.NewLabel("单点 NodeID"), d.verifyBtn, d.singleNode)
	full := container.NewBorder(nil, nil, widget.NewLabel("全量父节点"), container.NewHBox(d.fullBtn, d.stopReadBtn), d.allRoot)
	filters := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("筛选"), d.resultFilter, d.exportBtn, d.exportAttrBtn), copy, d.pointSummary)
	top := container.NewVBox(widget.NewLabel("节点清单（每行一个；浏览变量追加，结果选择用于单点操作）"), d.addresses, options, single, container.NewHBox(d.readBtn, d.pollBtn, d.stopPollBtn, d.importBtn), full, filters, d.scanStatus, d.jobBox, widget.NewSeparator())
	bottom := container.NewVBox(widget.NewSeparator(), container.NewBorder(nil, nil, d.allowWrite, d.writeBtn, d.writeValue), d.writeStatus)
	return container.NewBorder(top, bottom, nil, nil, d.table)
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
	d.browseTable = newTextTable(func() int { return len(d.snapshot.Nodes) + 1 }, d.browseCell, []float32{50, 400, 320, 210})
	d.browseTable.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 || id.Row > len(d.snapshot.Nodes) {
			return
		}
		n := d.snapshot.Nodes[id.Row-1]
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
	selection := container.NewBorder(nil, nil, container.NewHBox(d.selectAllBtn, d.clearSelectBtn, d.readSelectedBtn), nil, d.selectionLabel)
	top := container.NewVBox(container.NewBorder(nil, nil, widget.NewLabel("父节点"), container.NewHBox(d.browseBtn, d.browseFullBtn), d.browseRoot), widget.NewLabel("点击“选择”列勾选节点，可多选或全选后一起读取：对象读取其下全部变量，变量读取自身。点击其他列：对象继续浏览，变量追加到清单。Objects 根节点为 i=85。"), selection)
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

// newTextTable renders cells on demand, so only the visible rows are formatted.
func newTextTable(rows func() int, cell func(row, col int) string, widths []float32) *widget.Table {
	t := widget.NewTable(func() (int, int) { return rows(), len(widths) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		l.TextStyle = fyne.TextStyle{Bold: id.Row == 0}
		l.SetText(cell(id.Row, id.Col))
	})
	for i, w := range widths {
		t.SetColumnWidth(i, w)
	}
	return t
}

var browseHeader = []string{"选择", "NodeID", "显示名称", "节点类型"}

func (d *Desktop) browseCell(row, col int) string {
	if row == 0 {
		return browseHeader[col]
	}
	if row > len(d.snapshot.Nodes) {
		return ""
	}
	n := d.snapshot.Nodes[row-1]
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
