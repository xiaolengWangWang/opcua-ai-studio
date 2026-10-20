package ui

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
)

var packetHeader = []string{"时间", "请求", "方向", "操作", "状态", "耗时", "目标 / 错误"}

// packetFilters: 异常 is every request that did not succeed.
var packetFilters = []string{"全部", "异常", "READ", "WRITE", "BROWSE"}

var statusNames = map[device.Status]string{device.StatusSent: "已发送", device.StatusSuccess: "成功", device.StatusException: "异常", device.StatusTimeout: "超时", device.StatusCancelled: "已取消"}

// diagnosticsPanel lists the service requests newest first in a table that
// filters by kind and searches targets and errors; a selected request shows
// every target and result, next to the log.
func (d *Desktop) diagnosticsPanel() fyne.CanvasObject {
	d.logs = widget.NewLabel("")
	d.logs.Selectable = true
	d.logs.Wrapping = fyne.TextWrapWord
	refresh := func(string) { d.refreshPackets(true) }
	d.packetFilter = widget.NewSelect(packetFilters, refresh)
	d.packetFilter.SetSelected("全部")
	d.packetSearch = widget.NewEntry()
	d.packetSearch.SetPlaceHolder("按 NodeID 或错误搜索")
	d.packetSearch.OnChanged = refresh
	d.packetStats = widget.NewLabel("")
	d.packetStats.Importance = widget.LowImportance
	d.packetDetail = widget.NewLabel("点击一条记录，查看其全部目标和结果。")
	d.packetDetail.Selectable = true
	d.packetDetail.Wrapping = fyne.TextWrapWord
	d.packetTable = newTextTable(func() int { return len(d.packets) }, packetHeader, d.packetCell, d.packetStyle, []float32{100, 55, 45, 70, 70, 75, 480})
	d.packetTable.OnSelected = func(id widget.TableCellID) {
		if id.Row >= 0 && id.Row < len(d.packets) {
			d.packetDetail.SetText(packetDetail(d.packets[id.Row]))
		}
	}
	clear := widget.NewButtonWithIcon("清空显示", theme.ContentClearIcon(), func() {
		d.packetsSince = time.Now()
		d.packetDetail.SetText("")
		d.refreshPackets(true)
	})
	copyJSON := widget.NewButtonWithIcon("复制 JSON", theme.ContentCopyIcon(), d.copyEvents)
	export := widget.NewButtonWithIcon("导出 CSV", theme.DocumentSaveIcon(), d.exportPacketsDialog)
	copyLogs := widget.NewButtonWithIcon("复制日志", theme.ContentCopyIcon(), func() { d.win.Clipboard().SetContent(d.logs.Text) })
	bold := func(text string) *widget.Label {
		return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	hint := widget.NewLabel("服务级请求与响应记录（READ / WRITE / BROWSE），最近 256 条；完整记录保存在 SQLite，按天和 50 MB 轮转。")
	hint.Importance = widget.LowImportance
	hint.Wrapping = fyne.TextWrapWord
	toolbar := container.NewBorder(nil, nil, container.NewHBox(bold("筛选"), d.packetFilter, bold("搜索")), container.NewHBox(clear, copyJSON, export), d.packetSearch)
	detail := container.NewBorder(bold("请求详情"), nil, nil, nil, container.NewVScroll(d.packetDetail))
	logs := container.NewBorder(container.NewBorder(nil, nil, bold("运行日志"), copyLogs), nil, nil, nil, container.NewVScroll(d.logs))
	bottom := container.NewHSplit(detail, logs)
	split := container.NewVSplit(d.packetTable, bottom)
	split.SetOffset(.6)
	return container.NewBorder(container.NewVBox(hint, toolbar, d.packetStats), nil, nil, nil, split)
}

// refreshPackets rebuilds the filtered list when the packets or the filter
// change; force applies a changed filter.
func (d *Desktop) refreshPackets(force bool) {
	if d.packetTable == nil {
		return
	}
	pp := d.snapshot.Packets
	key := strconv.Itoa(len(pp))
	if len(pp) > 0 {
		last := pp[len(pp)-1]
		key += last.Time.String() + strconv.FormatUint(last.RequestID, 10) + string(last.Dir)
	}
	if key == d.packetsKey && !force {
		return
	}
	d.packetsKey = key
	filter := d.packetFilter.Selected
	match := device.Matcher(d.packetSearch.Text, false)
	d.packets = d.packets[:0]
	exceptions, replies := 0, 0
	var total, slowest time.Duration
	for i := len(pp) - 1; i >= 0; i-- {
		e := pp[i]
		if e.Time.Before(d.packetsSince) {
			continue
		}
		failed := e.Status != device.StatusSent && e.Status != device.StatusSuccess
		switch {
		case filter == "异常" && !failed:
			continue
		case filter != "全部" && filter != "异常" && e.Operation != filter:
			continue
		}
		fields := []string{e.Error, e.Operation}
		for _, a := range e.Targets {
			fields = append(fields, a.NodeID)
		}
		if !match(fields...) {
			continue
		}
		d.packets = append(d.packets, e)
		if failed {
			exceptions++
		}
		if e.Dir == device.DirRX {
			replies++
			total += e.RTT
			slowest = max(slowest, e.RTT)
		}
	}
	stats := fmt.Sprintf("显示 %d 条 · 异常 %d", len(d.packets), exceptions)
	if replies > 0 {
		stats += fmt.Sprintf(" · 响应平均 %s，最长 %s", duration(total/time.Duration(replies)), duration(slowest))
	}
	d.packetStats.SetText(stats)
	d.packetTable.UnselectAll()
	d.packetTable.Refresh()
}

func duration(t time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(t)/float64(time.Millisecond))
}

// targetSummary names the first three targets and the count of the rest.
func targetSummary(e device.Event) string {
	var names []string
	for _, a := range e.Targets[:min(len(e.Targets), 3)] {
		names = append(names, a.NodeID)
	}
	s := strings.Join(names, ", ")
	if n := max(len(e.Targets), e.Total); n > 3 {
		s += fmt.Sprintf(" 等 %d 个", n)
	}
	return s
}

func (d *Desktop) packetCell(row, col int) string {
	if row >= len(d.packets) {
		return ""
	}
	e := d.packets[row]
	switch col {
	case 0:
		return e.Time.Local().Format("15:04:05.000")
	case 1:
		return "#" + strconv.FormatUint(e.RequestID, 10)
	case 2:
		return string(e.Dir)
	case 3:
		return e.Operation
	case 4:
		return or(statusNames[e.Status], string(e.Status))
	case 5:
		if e.Dir == device.DirRX {
			return duration(e.RTT)
		}
		return ""
	}
	if e.Error != "" {
		return targetSummary(e) + " · " + e.Error
	}
	return targetSummary(e)
}

func (d *Desktop) packetStyle(row, col int) widget.Importance {
	if row >= len(d.packets) {
		return widget.MediumImportance
	}
	e := d.packets[row]
	switch {
	case col == 4 && e.Status == device.StatusSuccess:
		return widget.SuccessImportance
	case (col == 4 || col == 6) && e.Status != device.StatusSent && e.Status != device.StatusSuccess:
		return widget.DangerImportance
	case e.Dir == device.DirTX && col <= 3:
		return widget.LowImportance
	}
	return widget.MediumImportance
}

// packetDetail lists a request's targets with their results.
func packetDetail(e device.Event) string {
	lines := []string{fmt.Sprintf("#%d %s %s · %s · %s", e.RequestID, e.Dir, e.Operation, or(statusNames[e.Status], string(e.Status)), e.Time.Local().Format("2006-01-02 15:04:05.000"))}
	if e.Dir == device.DirRX {
		lines = append(lines, "耗时："+duration(e.RTT))
	}
	if e.ConnectionID != "" {
		lines = append(lines, "连接："+e.ConnectionID)
	}
	if e.Error != "" {
		lines = append(lines, "错误："+e.Error)
	}
	n := max(len(e.Targets), e.Total)
	head := fmt.Sprintf("目标（%d 个）：", n)
	if len(e.Targets) < n {
		head = fmt.Sprintf("目标（%d 个，记录前 %d 个）：", n, len(e.Targets))
	}
	lines = append(lines, head)
	for i, a := range e.Targets {
		line := "  " + a.NodeID
		if i < len(e.Results) {
			switch r := e.Results[i]; {
			case r.Read != nil:
				line += " → " + r.Read.Quality
				if r.Read.Value.Type != "" {
					line += " = " + device.FormatValue(r.Read.Value) + "（" + string(r.Read.Value.Type) + "）"
				}
				if r.Read.Error != "" {
					line += " · " + r.Read.Error
				}
			case r.Write != nil:
				line += " → " + r.Write.State
				if r.Write.Error != "" {
					line += " · " + r.Write.Error
				}
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// copyEvents copies the shown requests as JSON.
func (d *Desktop) copyEvents() {
	b, err := json.MarshalIndent(d.packets, "", "  ")
	if err != nil {
		d.showError(err)
		return
	}
	d.win.Clipboard().SetContent(string(b))
}

// exportPacketsDialog saves the shown requests as CSV.
func (d *Desktop) exportPacketsDialog() {
	packets := append([]device.Event(nil), d.packets...)
	if len(packets) == 0 {
		d.showError(fmt.Errorf("当前没有可导出的通讯记录"))
		return
	}
	d.saveFile(fmt.Sprintf("导出通讯记录 · %d 条", len(packets)), "opcua-packets-"+time.Now().Format("20060102-150405")+".csv", func(ctx context.Context, out io.Writer, report func(int, int)) (string, error) {
		if _, err := io.WriteString(out, string(rune(0xfeff))); err != nil {
			return "", err
		}
		w := csv.NewWriter(out)
		w.UseCRLF = true
		w.Write([]string{"时间", "请求", "连接", "方向", "操作", "状态", "耗时(ms)", "目标数", "目标", "错误"})
		for _, e := range packets {
			var targets []string
			for _, a := range e.Targets {
				targets = append(targets, a.NodeID)
			}
			rtt := ""
			if e.Dir == device.DirRX {
				rtt = strconv.FormatFloat(float64(e.RTT)/float64(time.Millisecond), 'f', 1, 64)
			}
			w.Write([]string{e.Time.Format(time.RFC3339Nano), strconv.FormatUint(e.RequestID, 10), e.ConnectionID, string(e.Dir), e.Operation, string(e.Status), rtt, strconv.Itoa(max(len(e.Targets), e.Total)), strings.Join(targets, "; "), e.Error})
		}
		w.Flush()
		return fmt.Sprintf("已导出 %d 条通讯记录。", len(packets)), w.Error()
	})
}
