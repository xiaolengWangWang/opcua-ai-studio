package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/assets/help"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
)

func (d *Desktop) singlePoint() (service.Point, error) {
	n, err := strconv.Atoi(d.count.Text)
	if err != nil {
		return service.Point{}, errors.New("元素数量须为整数")
	}
	node := d.singleNode.Text
	if strings.TrimSpace(node) == "" && !strings.Contains(strings.Trim(d.addresses.Text, "\r\n"), "\n") {
		node = strings.Trim(d.addresses.Text, "\r\n")
	}
	a, err := device.ParseAddress(node, device.DataType(d.kind.Selected), n)
	return service.Point{Address: a.NodeID, Type: a.Type, Count: a.Count}, err
}

func (d *Desktop) appendPoint(node string) {
	for _, line := range strings.Split(d.addresses.Text, "\n") {
		a, err := device.ParseAddress(strings.TrimSuffix(line, "\r"), "Auto", 1)
		if err == nil && a.NodeID == node {
			return
		}
	}
	list := strings.Trim(d.addresses.Text, "\r\n")
	if list != "" {
		list += "\n"
	}
	d.addresses.SetText(list + node)
}

func (d *Desktop) runCheck(fn func(context.Context) error, success func()) {
	if d.busy != 0 {
		return
	}
	if d.snapshot.State != "connected" {
		d.showError(errors.New("请先连接"))
		return
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.checkCancel = cancel
	d.controller.StopPolling()
	d.run(func(context.Context) error { defer cancel(); return fn(ctx) }, success)
}

func (d *Desktop) startFullRead(root string) {
	d.allRoot.SetText(root)
	d.tabs.SelectIndex(0)
	d.runCheck(func(ctx context.Context) error { return d.controller.ReadAll(ctx, root) }, nil)
}

// Filters lists the result filters; 不存在 and 可写 help check imported
// attribute sheets and control points, 含中文 finds Chinese NodeIDs.
var filters = []string{"全部", "GOOD", "BAD", "UNCERTAIN", "不存在", "可写（控制点位）", "含中文", "请求失败", "解析/类型错误", "未读取"}

func matchesPoint(r *device.ReadResult, filter string) bool {
	switch filter {
	case "全部":
		return true
	case "不存在":
		return r.Missing()
	case "可写（控制点位）":
		return r.Writable()
	case "含中文":
		return device.HasChinese(r.Address.NodeID)
	case "请求失败":
		return r.Execution == "REQUEST_FAILED"
	case "解析/类型错误":
		return r.Execution == "PARSE_FAILED" || r.Execution == "TYPE_MISMATCH"
	case "未读取", "NOT_READ":
		return r.Execution == "NOT_READ"
	default:
		return r.Quality == filter
	}
}

var pointHeader = []string{"NodeID", "类型", "读写", "值", "质量 / 状态码", "执行状态 / 时间 / 错误"}

var accessNames = map[string]string{"READ": "只读", "READ_WRITE": "读写", "WRITE": "只写", "NONE": "无权限"}

func pointRow(r *device.ReadResult) []string {
	detail := r.Execution
	if !r.SourceTime.IsZero() {
		detail += " · " + r.SourceTime.Local().Format("15:04:05.000")
	}
	if r.Error != "" {
		detail += " · " + r.Error
	}
	kind, value := string(r.Kind()), device.FormatValue(r.Value)
	if kind == "" {
		kind = string(r.Address.Type)
	}
	if r.Value.Type == "" {
		value = ""
	}
	quality := r.Quality
	if quality != "UNKNOWN" {
		quality += fmt.Sprintf(" / 0x%08X", r.StatusCode)
	}
	return []string{r.Address.NodeID, kind, accessNames[r.Access], value, quality, detail}
}

// pointCell renders one visible cell on demand: formatting every row of a
// 50000-point list on each refresh stalled the UI thread.
func (d *Desktop) pointCell(row, col int) string {
	if row >= len(d.visible) {
		return ""
	}
	return pointRow(&d.snapshot.Results[d.visible[row]])[col]
}

// pointStyle colors quality by severity, marks control points and flags
// client-side failures, so problems stand out in long lists.
func (d *Desktop) pointStyle(row, col int) widget.Importance {
	if row >= len(d.visible) {
		return widget.MediumImportance
	}
	r := &d.snapshot.Results[d.visible[row]]
	switch {
	case col == 2 && r.Writable():
		return widget.HighImportance
	case col == 4 && r.Quality == "GOOD":
		return widget.SuccessImportance
	case col == 4 && r.Quality == "BAD":
		return widget.DangerImportance
	case col == 4 && r.Quality == "UNCERTAIN":
		return widget.WarningImportance
	case col == 5 && r.Execution == "NOT_READ":
		return widget.LowImportance
	case col == 5 && r.Execution != "READ":
		return widget.WarningImportance
	}
	return widget.MediumImportance
}

// summarySegment is one inline part of the colored result summary.
func summarySegment(text string, color fyne.ThemeColorName) widget.RichTextSegment {
	return &widget.TextSegment{Text: text, Style: widget.RichTextStyle{Inline: true, ColorName: color, TextStyle: fyne.TextStyle{Bold: color != theme.ColorNamePlaceHolder}}}
}

func (d *Desktop) refreshPointRows() {
	filter := d.resultFilter.Selected
	rr := d.snapshot.Results
	// Results are copy-on-write, so an unchanged slice needs no new rows.
	key := pointsKey{len(rr), filter, nil}
	if len(rr) > 0 {
		key.first = &rr[0]
	}
	if key == d.pointsKey {
		return
	}
	d.pointsKey = key
	d.visible = d.visible[:0]
	counts := map[string]int{}
	for i := range rr {
		r := &rr[i]
		counts[r.Quality]++
		counts[r.Execution]++
		if r.Missing() {
			counts["MISSING"]++
		}
		if r.Writable() {
			counts["WRITABLE"]++
		}
		if device.HasChinese(r.Address.NodeID) {
			counts["CHINESE"]++
		}
		if matchesPoint(r, filter) {
			d.visible = append(d.visible, i)
		}
	}
	d.table.UnselectAll()
	d.table.Refresh()
	sep := summarySegment("  ·  ", theme.ColorNamePlaceHolder)
	d.pointSummary.Segments = []widget.RichTextSegment{
		summarySegment(fmt.Sprintf("共 %d", len(rr)), theme.ColorNameForeground), sep,
		summarySegment(fmt.Sprintf("GOOD %d", counts["GOOD"]), theme.ColorNameSuccess), sep,
		summarySegment(fmt.Sprintf("BAD %d", counts["BAD"]), theme.ColorNameError), sep,
		summarySegment(fmt.Sprintf("UNCERTAIN %d", counts["UNCERTAIN"]), theme.ColorNameWarning), sep,
		summarySegment(fmt.Sprintf("不存在 %d", counts["MISSING"]), theme.ColorNameError), sep,
		summarySegment(fmt.Sprintf("可写 %d", counts["WRITABLE"]), theme.ColorNamePrimary), sep,
		summarySegment(fmt.Sprintf("含中文 %d", counts["CHINESE"]), theme.ColorNameForeground), sep,
		summarySegment(fmt.Sprintf("请求失败 %d / 解析或类型错误 %d / 未读 %d", counts["REQUEST_FAILED"], counts["PARSE_FAILED"]+counts["TYPE_MISMATCH"], counts["NOT_READ"]), theme.ColorNamePlaceHolder),
	}
	d.pointSummary.Refresh()
	if d.jobFollow && len(rr) > 0 {
		d.jobBar.SetValue(1 - float64(counts["NOT_READ"])/float64(len(rr)))
	}
}

type pointsKey struct {
	n      int
	filter string
	first  *device.ReadResult
}

func (d *Desktop) copyPointRows() {
	lines := []string{strings.Join(pointHeader, "\t")}
	for _, i := range d.visible {
		lines = append(lines, strings.Join(pointRow(&d.snapshot.Results[i]), "\t"))
	}
	d.win.Clipboard().SetContent(strings.Join(lines, "\n"))
}

func (d *Desktop) exportPointsDialog() {
	// The dialog can remain open while polling; export exactly this snapshot.
	snapshot := d.snapshot
	filter := d.resultFilter.Selected
	count := 0
	for i := range snapshot.Results {
		if matchesPoint(&snapshot.Results[i], filter) {
			count++
		}
	}
	if count == 0 {
		d.showError(errors.New("当前筛选没有可导出的点位"))
		return
	}
	d.saveFile(fmt.Sprintf("导出 CSV · %s · %d 行", filter, count), "opcua-points-"+time.Now().Format("20060102-150405")+".csv", func(ctx context.Context, out io.Writer, report func(int, int)) (string, error) {
		err := exportPointsCSV(ctx, out, snapshot, filter, func(done int) { report(done, count) })
		return fmt.Sprintf("已导出 %d 行（%s）。", count, filter), err
	})
}

func (d *Desktop) showPointHelp() {
	text := widget.NewRichTextFromMarkdown(help.Points)
	text.Wrapping = fyne.TextWrapWord
	box := dialog.NewCustom("点位读取帮助", "关闭", container.NewVScroll(text), d.win)
	box.Resize(fyne.NewSize(760, 550))
	box.Show()
}
