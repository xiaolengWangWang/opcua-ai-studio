package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/service"
)

// background runs a record query off the UI thread and shows its result.
func (d *Desktop) background(query func() error, show func()) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.workers.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.workers.Done()
		err := query()
		fyne.Do(func() {
			switch {
			case d.ctx.Err() != nil:
			case err != nil:
				d.showError(fmt.Errorf("读取记录失败：%w", err))
			default:
				show()
			}
		})
	}()
}

// connectionHistory is how many connections the record dialog lists.
const connectionHistory = 500

// connectionHistoryDialog lists the connections kept in the record
// database, newest first: when, where, with which security and account,
// the outcome, the server and how long it lasted. A connection can be
// filled in, connected again, or opened with its run log and requests.
func (d *Desktop) connectionHistoryDialog() {
	var all []service.Connection
	d.background(func() (err error) {
		all, err = d.controller.Connections(connectionHistory)
		return err
	}, func() { d.showConnections(all) })
}

var connectionHeader = []string{"时间", "地址", "安全", "账号", "结果", "服务器", "时长"}

func (d *Desktop) showConnections(all []service.Connection) {
	shown := all
	var chosen *service.Connection
	detail := widget.NewLabel("点击一条连接，查看其设置和服务器信息。")
	detail.Wrapping = fyne.TextWrapWord
	detail.Selectable = true
	cell := func(row, col int) string {
		if row >= len(shown) {
			return ""
		}
		c := shown[row]
		switch col {
		case 0:
			return c.Time.Local().Format("01-02 15:04:05")
		case 1:
			return strings.TrimPrefix(c.Target, "opc.tcp://")
		case 2:
			return c.Policy + " / " + c.Mode
		case 3:
			return or(c.User, "匿名")
		case 4:
			return connectionResult(c)
		case 5:
			return or(c.Server, "—")
		}
		return d.connectionLength(c)
	}
	style := func(row, col int) widget.Importance {
		if col == 4 && row < len(shown) {
			if shown[row].OK {
				return widget.SuccessImportance
			}
			return widget.DangerImportance
		}
		return widget.MediumImportance
	}
	table := newTextTable(func() int { return len(shown) }, connectionHeader, cell, style, []float32{110, 190, 170, 70, 80, 170, 80})
	fill := widget.NewButtonWithIcon("填入", theme.ContentPasteIcon(), nil)
	again := widget.NewButtonWithIcon("连接", theme.LoginIcon(), nil)
	again.Importance = widget.HighImportance
	records := widget.NewButtonWithIcon("查看日志与报文", theme.DocumentIcon(), nil)
	buttons := []*widget.Button{fill, again, records}
	for _, b := range buttons {
		b.Disable()
	}
	filter := widget.NewSelect(nil, nil)
	count := widget.NewLabel("")
	apply := func(choice string) {
		shown = all
		if target := normalizeEndpoint(d.endpoint()); choice != "全部连接" && target != "" {
			shown = nil
			for _, c := range all {
				if c.Target == target {
					shown = append(shown, c)
				}
			}
		}
		chosen = nil
		for _, b := range buttons {
			b.Disable()
		}
		count.SetText(fmt.Sprintf("%d 条", len(shown)))
		table.UnselectAll()
		table.Refresh()
	}
	options := []string{"全部连接"}
	if target := d.endpoint(); target != "" {
		options = append(options, "当前地址 "+strings.TrimPrefix(normalizeEndpoint(target), "opc.tcp://"))
	}
	filter.Options = options
	filter.OnChanged = apply
	filter.SetSelected(options[len(options)-1])
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(shown) {
			return
		}
		c := shown[id.Row]
		chosen = &c
		detail.SetText(connectionDetail(c, d.connectionLength(c)))
		for _, b := range buttons {
			b.Enable()
		}
		if c.Session == 0 {
			records.Disable()
		}
	}
	var box dialog.Dialog
	use := func(connect bool) {
		if chosen == nil {
			return
		}
		d.setEndpoint(chosen.Target)
		d.profile.ClearSelected()
		d.policy.SetSelected(chosen.Policy)
		d.mode.SetSelected(chosen.Mode)
		d.username.SetText(chosen.User)
		box.Hide()
		if connect {
			d.connectBtn.OnTapped()
		}
	}
	fill.OnTapped = func() { use(false) }
	again.OnTapped = func() { use(true) }
	records.OnTapped = func() {
		if chosen != nil {
			d.sessionRecordsDialog(*chosen)
		}
	}
	top := container.NewBorder(nil, nil, widget.NewLabelWithStyle("显示", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), count, filter)
	bottom := container.NewBorder(nil, container.NewHBox(fill, again, records), nil, nil, container.NewVScroll(detail))
	split := container.NewVSplit(table, bottom)
	split.SetOffset(.55)
	note := widget.NewLabel("连接记录保存在记录数据库中，与通讯记录一起按天、每个文件不超过 50 MB 保存；密码不保存。")
	note.Wrapping = fyne.TextWrapWord
	box = dialog.NewCustom("连接记录", "关闭", container.NewBorder(container.NewVBox(note, top), nil, nil, nil, split), d.win)
	box.Resize(fyne.NewSize(980, 640))
	box.Show()
}

func connectionResult(c service.Connection) string {
	switch {
	case !c.OK:
		return "失败"
	case c.Reconnect:
		return "重连成功"
	}
	return "成功"
}

// connectionLength is how long a connection lasted, or that it is open.
func (d *Desktop) connectionLength(c service.Connection) string {
	switch {
	case !c.OK:
		return "—"
	case !c.End.IsZero():
		return lasted(c.End.Sub(c.Time))
	case c.Session != 0 && c.Session == d.snapshot.Session && d.snapshot.State == "connected":
		return "进行中"
	}
	return "未正常断开"
}

func lasted(t time.Duration) string {
	switch {
	case t < time.Minute:
		return fmt.Sprintf("%d 秒", int(t.Seconds()))
	case t < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(t.Minutes()), int(t.Seconds())%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", int(t.Hours()), int(t.Minutes())%60)
}

// connectionDetail describes a connection with the server information it
// read.
func connectionDetail(c service.Connection, length string) string {
	lines := []string{
		"时间：" + c.Time.Local().Format("2006-01-02 15:04:05"),
		"Endpoint：" + c.Target,
		fmt.Sprintf("安全：%s / %s · 账号：%s", c.Policy, c.Mode, or(c.User, "匿名")),
		"结果：" + connectionResult(c) + " · 时长：" + length,
	}
	if c.Error != "" {
		lines = append(lines, "错误："+c.Error)
	}
	var info opcua.ServerInfo
	if c.Detail != "" && json.Unmarshal([]byte(c.Detail), &info) == nil {
		lines = append(lines, "", serverInfoText(info))
	}
	return strings.Join(lines, "\n")
}

// sessionRecordsDialog shows one connection's run log and its last requests
// from the record database.
func (d *Desktop) sessionRecordsDialog(c service.Connection) {
	var lines []service.LogLine
	var packets []device.Event
	d.background(func() (err error) {
		lines, packets, err = d.controller.SessionRecords(c.Session, 2000)
		return err
	}, func() {
		logText := widget.NewLabel(logLines(lines, false))
		logText.Selectable = true
		logText.Wrapping = fyne.TextWrapWord
		// Newest first, as in the request table.
		for i, j := 0, len(packets)-1; i < j; i, j = i+1, j-1 {
			packets[i], packets[j] = packets[j], packets[i]
		}
		detail := widget.NewLabel("点击一条请求，查看其全部目标和结果。")
		detail.Selectable = true
		detail.Wrapping = fyne.TextWrapWord
		table := newTextTable(func() int { return len(packets) }, packetHeader, func(row, col int) string {
			if row >= len(packets) {
				return ""
			}
			return packetText(packets[row], col)
		}, func(row, col int) widget.Importance {
			if row >= len(packets) {
				return widget.MediumImportance
			}
			return packetImportance(packets[row], col)
		}, []float32{100, 55, 45, 70, 70, 75, 420})
		table.OnSelected = func(id widget.TableCellID) {
			if id.Row >= 0 && id.Row < len(packets) {
				detail.SetText(packetDetail(packets[id.Row]))
			}
		}
		split := container.NewVSplit(table, container.NewVScroll(detail))
		split.SetOffset(.6)
		tabs := container.NewAppTabs(
			container.NewTabItem(fmt.Sprintf("运行日志（%d）", len(lines)), container.NewVScroll(logText)),
			container.NewTabItem(fmt.Sprintf("请求（最近 %d 条）", len(packets)), split),
		)
		title := fmt.Sprintf("%s · %s 的日志与报文", strings.TrimPrefix(c.Target, "opc.tcp://"), c.Time.Local().Format("01-02 15:04"))
		box := dialog.NewCustom(title, "关闭", tabs, d.win)
		box.Resize(fyne.NewSize(960, 620))
		box.Show()
	})
}

func logLines(lines []service.LogLine, day bool) string {
	if len(lines) == 0 {
		return "没有记录。"
	}
	layout := "15:04:05"
	if day {
		layout = "01-02 15:04:05"
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Time.Local().Format(layout) + " " + l.Text
	}
	return strings.Join(out, "\n")
}

// logHistoryDialog shows the latest run log lines of every window from the
// record database, including earlier runs of the program.
func (d *Desktop) logHistoryDialog() {
	var lines []service.LogLine
	d.background(func() (err error) {
		lines, err = d.controller.LogHistory(2000)
		return err
	}, func() {
		text := logLines(lines, true)
		label := widget.NewLabel(text)
		label.Selectable = true
		label.Wrapping = fyne.TextWrapWord
		copyLogs := widget.NewButtonWithIcon("复制", theme.ContentCopyIcon(), func() { d.win.Clipboard().SetContent(text) })
		scroll := container.NewVScroll(label)
		box := dialog.NewCustom(fmt.Sprintf("历史日志（最近 %d 行）", len(lines)), "关闭", container.NewBorder(nil, container.NewHBox(copyLogs), nil, nil, scroll), d.win)
		box.Resize(fyne.NewSize(900, 600))
		box.Show()
		scroll.ScrollToBottom()
	})
}

// qualityHelp explains the counts above the results: what each means and
// what to do about it.
var qualityHelp = [][3]string{
	{"GOOD", "服务器返回的质量正常，数值可用。", "可放心采集；导出属性表时选“仅 GOOD 点位”。"},
	{"BAD", "服务器返回的质量错误：设备离线（BadNoCommunication）、无权限、节点不存在等，数值不可用。", "看“执行状态 / 错误”列的原因；设备离线请厂家检查设备或网关。"},
	{"UNCERTAIN", "服务器不能确定数值是否可靠，如传感器超量程、数据过期。", "与厂家确认点位状态，不宜直接用于控制。"},
	{"不存在", "服务器上找不到该 NodeID（BadNodeIdUnknown / BadNodeIdInvalid）。", "核对命名空间和标识，或在“节点浏览”中搜索正确的 NodeID。"},
	{"可写", "当前账号可写入的点位，即控制下发点位。", "点击其“值”或“写入”按钮可直接写入验证；写入前须确认允许。"},
	{"含中文", "NodeID 含中文的点位。", "中文须与服务器完全一致，建议从浏览结果点选，不要手工输入。"},
	{"请求失败", "请求没有得到服务器响应（UNKNOWN），如超时、断线。", "检查网络和服务器负载后重新读取。"},
	{"解析或类型错误", "数值无法解析，或与所选、属性表中的类型不一致（TYPE_MISMATCH）。", "按服务器类型修改类型或属性标识，见手册附录的类型对照。"},
	{"未读", "因停止或中断尚未读取的点位（NOT_READ）。", "重新读取即可。"},
}

// showQualityHelp explains what GOOD, BAD and the other counts mean.
func (d *Desktop) showQualityHelp() {
	rows := container.NewVBox()
	colors := map[string]widget.Importance{"GOOD": widget.SuccessImportance, "BAD": widget.DangerImportance, "UNCERTAIN": widget.WarningImportance, "不存在": widget.DangerImportance, "可写": widget.HighImportance, "请求失败": widget.DangerImportance, "解析或类型错误": widget.WarningImportance}
	for _, q := range qualityHelp {
		name := widget.NewLabelWithStyle(q[0], fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		name.Importance = colors[q[0]]
		text := widget.NewLabel("含义：" + q[1] + "\n作用：" + q[2])
		text.Wrapping = fyne.TextWrapWord
		rows.Add(container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(120, name.MinSize().Height), name), nil, text))
		rows.Add(widget.NewSeparator())
	}
	intro := widget.NewLabel("结果上方的统计按服务器返回的 OPC 质量和程序的执行结果计数，点击“筛选”可只看某一类。")
	intro.Wrapping = fyne.TextWrapWord
	box := dialog.NewCustom("质量与统计说明", "关闭", container.NewBorder(intro, nil, nil, nil, container.NewVScroll(rows)), d.win)
	box.Resize(fyne.NewSize(720, 600))
	box.Show()
}
