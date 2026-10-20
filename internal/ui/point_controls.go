package ui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
)

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
	d.rememberRoot(root)
	d.tabs.SelectIndex(0)
	d.runCheck(func(ctx context.Context) error { return d.controller.ReadAll(ctx, root) }, nil)
}

// rememberRoot keeps a parent at the head of the ten used last.
func (d *Desktop) rememberRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	list := append([]string{root}, slices.DeleteFunc(d.recentRoots(), func(r string) bool { return r == root })...)
	d.app.Preferences().SetString("recent-roots", strings.Join(list[:min(len(list), 10)], "\n"))
	d.rootOptionsKey = ""
	d.refreshRootOptions()
}

func (d *Desktop) recentRoots() []string {
	return strings.FieldsFunc(d.app.Preferences().String("recent-roots"), func(r rune) bool { return r == '\n' })
}

// refreshRootOptions offers as parents the Objects folder, the connected
// server's namespaces and the parents used last.
func (d *Desktop) refreshRootOptions() {
	options := []string{"i=85"}
	if s := d.snapshot.Server; s != nil {
		for i := 1; i < len(s.Namespaces); i++ {
			options = append(options, fmt.Sprintf("ns=%d", i))
		}
	}
	for _, r := range d.recentRoots() {
		if !slices.Contains(options, r) {
			options = append(options, r)
		}
	}
	if key := strings.Join(options, "\n"); key != d.rootOptionsKey {
		d.rootOptionsKey = key
		d.allRoot.SetOptions(options)
		d.browseRoot.SetOptions(options)
	}
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

var pointHeader = []string{"NodeID", "名称", "类型", "读写", "值", "质量 / 状态码", "时间", "执行状态 / 错误"}

// pointWidths fit a 1280 pixels wide window beside the side column.
var pointWidths = []float32{220, 110, 64, 46, 130, 150, 90, 220}

var accessNames = map[string]string{"READ": "只读", "READ_WRITE": "读写", "WRITE": "只写", "NONE": "无权限"}

func pointRow(r *device.ReadResult) []string {
	detail := r.Execution
	if r.Error != "" {
		detail += " · " + r.Error
	}
	// The source time, else the server's, else when the value was read.
	stamp := ""
	for _, t := range []time.Time{r.SourceTime, r.ServerTime, r.Time} {
		if !t.IsZero() {
			stamp = t.Local().Format("15:04:05.000")
			break
		}
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
	return []string{r.Address.NodeID, r.DisplayName, kind, accessNames[r.Access], value, quality, stamp, detail}
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
	switch col {
	case 3:
		if r.Writable() {
			return widget.HighImportance
		}
	case 4:
		// A value that changed recently, as polling shows it, stands out.
		if t, ok := d.changedAt[r.Address.NodeID]; ok && time.Since(t) < changeHighlight {
			return widget.HighImportance
		}
	case 5:
		return qualityImportance(r.Quality)
	case 7:
		if r.Execution == "NOT_READ" {
			return widget.LowImportance
		}
		if r.Execution != "READ" {
			return widget.WarningImportance
		}
	}
	return widget.MediumImportance
}

func qualityImportance(quality string) widget.Importance {
	switch quality {
	case "GOOD":
		return widget.SuccessImportance
	case "BAD":
		return widget.DangerImportance
	case "UNCERTAIN":
		return widget.WarningImportance
	}
	return widget.MediumImportance
}

// summarySegment is one inline part of the colored result summary.
func summarySegment(text string, color fyne.ThemeColorName) widget.RichTextSegment {
	return &widget.TextSegment{Text: text, Style: widget.RichTextStyle{Inline: true, ColorName: color, TextStyle: fyne.TextStyle{Bold: color != theme.ColorNamePlaceHolder}}}
}

func (d *Desktop) refreshPointRows() {
	q := d.query()
	rr := d.snapshot.Results
	// Results are copy-on-write, so an unchanged slice needs no new rows.
	key := pointsKey{len(rr), q, nil, d.sortCol, d.sortDesc}
	if len(rr) > 0 {
		key.first = &rr[0]
	}
	if key == d.pointsKey {
		return
	}
	d.pointsKey = key
	d.visible = d.visible[:0]
	keep := q.matcher()
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
		if keep(r) {
			d.visible = append(d.visible, i)
		}
	}
	d.trackChanges(rr)
	d.recordHistory(rr)
	if d.sortCol >= 0 {
		d.sortVisible()
	}
	d.table.UnselectAll()
	d.table.Refresh()
	if len(rr) == 0 {
		d.emptyHint.Show()
	} else {
		d.emptyHint.Hide()
	}
	d.refreshTree()
	d.refreshViews()
	d.refreshSingleOptions()
	total := fmt.Sprintf("共 %d", len(rr))
	if len(d.visible) != len(rr) {
		total = fmt.Sprintf("显示 %d / 共 %d", len(d.visible), len(rr))
	}
	sep := summarySegment("  ·  ", theme.ColorNamePlaceHolder)
	// 共, GOOD and BAD always show; the rarer counts only when not zero, so
	// the summary stays on one line above the data.
	d.summaryBase = []widget.RichTextSegment{
		summarySegment(total, theme.ColorNameForeground), sep,
		summarySegment(fmt.Sprintf("GOOD %d", counts["GOOD"]), theme.ColorNameSuccess), sep,
		summarySegment(fmt.Sprintf("BAD %d", counts["BAD"]), theme.ColorNameError),
	}
	for _, c := range []struct {
		name  string
		n     int
		color fyne.ThemeColorName
	}{
		{"UNCERTAIN", counts["UNCERTAIN"], theme.ColorNameWarning},
		{"不存在", counts["MISSING"], theme.ColorNameError},
		{"可写", counts["WRITABLE"], theme.ColorNamePrimary},
		{"含中文", counts["CHINESE"], theme.ColorNameForeground},
		{"请求失败", counts["REQUEST_FAILED"], theme.ColorNameError},
		{"解析或类型错误", counts["PARSE_FAILED"] + counts["TYPE_MISMATCH"], theme.ColorNameWarning},
		{"未读", counts["NOT_READ"], theme.ColorNamePlaceHolder},
	} {
		if c.n > 0 {
			d.summaryBase = append(d.summaryBase, sep, summarySegment(fmt.Sprintf("%s %d", c.name, c.n), c.color))
		}
	}
	d.updateSummary()
	if d.jobFollow && len(rr) > 0 {
		d.jobBar.SetValue(1 - float64(counts["NOT_READ"])/float64(len(rr)))
	}
}

type pointsKey struct {
	n        int
	q        pointQuery
	first    *device.ReadResult
	sortCol  int
	sortDesc bool
}

// updateSummary shows the counts and, on the same line, the latest read
// status, so both take one row above the data.
func (d *Desktop) updateSummary() {
	segments := append([]widget.RichTextSegment(nil), d.summaryBase...)
	if s := d.snapshot.ScanStatus; s != "" {
		segments = append(segments, summarySegment("  |  "+s, theme.ColorNamePlaceHolder))
	}
	d.pointSummary.Segments = segments
	d.pointSummary.Refresh()
}

// selectedResult is the result last chosen in a view, if it is still shown.
func (d *Desktop) selectedResult() (device.ReadResult, bool) {
	if d.selectedNode == "" {
		return device.ReadResult{}, false
	}
	for i := range d.snapshot.Results {
		if d.snapshot.Results[i].Address.NodeID == d.selectedNode {
			return d.snapshot.Results[i], true
		}
	}
	return device.ReadResult{}, false
}

// changeHighlight is how long a changed value stays marked; values are
// compared for lists up to changeTrackLimit points, as polled lists are.
const (
	changeHighlight  = 3 * time.Second
	changeTrackLimit = 5000
)

// trackChanges notes the points whose value differs from the last results.
func (d *Desktop) trackChanges(rr []device.ReadResult) {
	if len(rr) > changeTrackLimit {
		d.lastValues = nil
		return
	}
	if d.changedAt == nil {
		d.changedAt = map[string]time.Time{}
	}
	now := time.Now()
	values := make(map[string]string, len(rr))
	for i := range rr {
		id, v := rr[i].Address.NodeID, device.FormatValue(rr[i].Value)
		if old, ok := d.lastValues[id]; ok && old != v && rr[i].Value.Type != "" {
			d.changedAt[id] = now
		}
		values[id] = v
	}
	d.lastValues = values
	for id, t := range d.changedAt {
		if now.Sub(t) > changeHighlight {
			delete(d.changedAt, id)
		}
	}
}

// qualityRank sorts problems first: BAD, UNCERTAIN, UNKNOWN, then GOOD.
var qualityRank = map[string]string{"BAD": "0", "UNCERTAIN": "1", "UNKNOWN": "2", "GOOD": "3"}

// sortVisible orders the shown rows by the sort column: values that are
// numbers by value and before text, quality by severity, the rest as text.
func (d *Desktop) sortVisible() {
	type key struct {
		text   string
		number float64
		isNum  bool
	}
	keys := make(map[int]key, len(d.visible))
	for _, i := range d.visible {
		r := &d.snapshot.Results[i]
		k := key{text: pointRow(r)[d.sortCol]}
		switch d.sortCol {
		case 4:
			k.number, k.isNum = numericValue(r.Value)
		case 5:
			k.text = qualityRank[r.Quality] + k.text
		}
		keys[i] = k
	}
	slices.SortStableFunc(d.visible, func(a, b int) int {
		x, y := keys[a], keys[b]
		c := 0
		switch {
		case x.isNum && y.isNum:
			c = cmp.Compare(x.number, y.number)
		case x.isNum != y.isNum:
			c = 1
			if x.isNum {
				c = -1
			}
		default:
			c = strings.Compare(x.text, y.text)
		}
		if d.sortDesc {
			return -c
		}
		return c
	})
}

func numericValue(v device.Value) (float64, bool) {
	switch {
	case v.Elements != nil:
		return 0, false
	case v.Integer != "":
		f, err := strconv.ParseFloat(v.Integer, 64)
		return f, err == nil
	case v.Type == "Float" || v.Type == "Double":
		return v.Real, true
	}
	return 0, false
}

// sortBy cycles a column through ascending, descending and unsorted.
func (d *Desktop) sortBy(col int) {
	switch {
	case d.sortCol != col:
		d.sortCol, d.sortDesc = col, false
	case !d.sortDesc:
		d.sortDesc = true
	default:
		d.sortCol = -1
	}
	d.refreshPointRows()
	d.table.Refresh()
}

func (d *Desktop) sortMark(col int) string {
	switch {
	case col != d.sortCol:
		return ""
	case d.sortDesc:
		return " ▼"
	}
	return " ▲"
}

func (d *Desktop) copyPointRows() {
	lines := []string{strings.Join(pointHeader, "\t")}
	for _, i := range d.visible {
		lines = append(lines, strings.Join(pointRow(&d.snapshot.Results[i]), "\t"))
	}
	d.win.Clipboard().SetContent(strings.Join(lines, "\n"))
}

// exportPointsDialog exports the rows the filter and search show.
func (d *Desktop) exportPointsDialog() {
	// The dialog can remain open while polling; export exactly this snapshot.
	snapshot := d.snapshot
	q := d.query()
	keep := q.matcher()
	count := 0
	for i := range snapshot.Results {
		if keep(&snapshot.Results[i]) {
			count++
		}
	}
	if count == 0 {
		d.showError(errors.New("当前筛选和搜索没有可导出的点位"))
		return
	}
	d.saveFile(fmt.Sprintf("导出 CSV · %s · %d 行", q.label(), count), "opcua-points-"+time.Now().Format("20060102-150405")+".csv", func(ctx context.Context, out io.Writer, report func(int, int)) (string, error) {
		err := exportPointsCSV(ctx, out, snapshot, q, func(done int) { report(done, count) })
		return fmt.Sprintf("已导出 %d 行（%s）。", count, q.label()), err
	})
}

func (d *Desktop) showPointHelp() {
	text := widget.NewRichTextFromMarkdown(help.Points)
	text.Wrapping = fyne.TextWrapWord
	box := dialog.NewCustom("OPC UA AI Studio 操作手册", "关闭", container.NewVScroll(text), d.win)
	box.Resize(fyne.NewSize(900, 680))
	box.Show()
}
