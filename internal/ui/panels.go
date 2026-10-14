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
	row := container.NewBorder(nil, nil, widget.NewLabel("Endpoint"), container.NewHBox(d.discoverBtn, d.connectBtn, d.disconnectBtn), d.target)
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
		d.run(func(ctx context.Context) error { return d.controller.Read(ctx, pp) }, d.saveInputs)
	}
	d.readBtn = widget.NewButtonWithIcon("读取", theme.ViewRefreshIcon(), read)
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
	d.rows = [][]string{{"NodeID", "类型", "值", "质量 / 状态码", "源时间 / 错误"}}
	d.table = newTextTable(func() [][]string { return d.rows }, []float32{260, 85, 160, 195, 270})
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 || id.Row > len(d.snapshot.Results) {
			return
		}
		r := d.snapshot.Results[id.Row-1]
		d.addresses.SetText(r.Address.NodeID)
		d.kind.SetSelected(string(r.Value.Type))
		n := 1
		if r.Value.Elements != nil {
			n = len(r.Value.Elements)
			if n == 0 {
				n = 1
			}
		}
		d.count.SetText(fmt.Sprint(n))
	}
	copy := widget.NewButton("复制结果", func() {
		var lines []string
		for _, row := range d.rows {
			lines = append(lines, strings.Join(row, "\t"))
		}
		d.win.Clipboard().SetContent(strings.Join(lines, "\n"))
	})
	options := container.NewGridWithColumns(3, widget.NewForm(widget.NewFormItem("类型", d.kind)), widget.NewForm(widget.NewFormItem("数量", d.count)), widget.NewForm(widget.NewFormItem("轮询间隔", d.interval)))
	top := container.NewVBox(widget.NewLabel("节点清单（每行一个，可从节点浏览或结果表中选择）"), d.addresses, options, container.NewHBox(d.readBtn, d.pollBtn, d.stopPollBtn, copy), widget.NewSeparator())
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
	d.browseRows = [][]string{{"NodeID", "显示名称", "节点类型"}}
	d.browseTable = newTextTable(func() [][]string { return d.browseRows }, []float32{430, 340, 230})
	d.browseTable.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 || id.Row > len(d.snapshot.Nodes) {
			return
		}
		n := d.snapshot.Nodes[id.Row-1]
		if strings.Contains(n.Class, "Variable") {
			d.addresses.SetText(n.NodeID)
			d.kind.SetSelected("Auto")
			d.count.SetText("1")
			d.tabs.SelectIndex(0)
		} else {
			d.browseRoot.SetText(n.NodeID)
		}
	}
	top := container.NewVBox(container.NewBorder(nil, nil, widget.NewLabel("父节点"), d.browseBtn, d.browseRoot), widget.NewLabel("点击对象填入父节点后继续浏览；点击变量加入读写页。Objects 根节点为 i=85。"))
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
func newTextTable(rows func() [][]string, widths []float32) *widget.Table {
	t := widget.NewTable(func() (int, int) { return len(rows()), len(widths) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(id widget.TableCellID, o fyne.CanvasObject) {
		r := rows()
		l := o.(*widget.Label)
		if id.Row < len(r) && id.Col < len(r[id.Row]) {
			l.SetText(r[id.Row][id.Col])
			l.TextStyle = fyne.TextStyle{Bold: id.Row == 0}
		}
	})
	for i, w := range widths {
		t.SetColumnWidth(i, w)
	}
	return t
}
