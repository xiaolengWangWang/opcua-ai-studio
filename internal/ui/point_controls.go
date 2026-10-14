package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

func matchesPoint(r device.ReadResult, filter string) bool {
	switch filter {
	case "全部":
		return true
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

func (d *Desktop) refreshPointRows() {
	filter := d.resultFilter.Selected
	d.visibleResults = nil
	d.rows = [][]string{{"NodeID", "类型", "值", "质量 / 状态码", "执行状态 / 时间 / 错误"}}
	counts := map[string]int{}
	for _, r := range d.snapshot.Results {
		counts[r.Quality]++
		counts[r.Execution]++
		if !matchesPoint(r, filter) {
			continue
		}
		d.visibleResults = append(d.visibleResults, r)
		detail := r.Execution
		if !r.SourceTime.IsZero() {
			detail += " · " + r.SourceTime.Local().Format("15:04:05.000")
		}
		if r.Error != "" {
			detail += " · " + r.Error
		}
		kind := string(r.Value.Type)
		value := device.FormatValue(r.Value)
		if kind == "" {
			kind = string(r.Address.Type)
			value = ""
		}
		quality := r.Quality
		if quality != "UNKNOWN" {
			quality += fmt.Sprintf(" / 0x%08X", r.StatusCode)
		}
		d.rows = append(d.rows, []string{r.Address.NodeID, kind, value, quality, detail})
	}
	d.table.UnselectAll()
	d.table.Refresh()
	d.pointSummary.SetText(fmt.Sprintf("共 %d · GOOD %d / BAD %d / UNCERTAIN %d · 请求失败 %d / 解析或类型错误 %d / 未读 %d", len(d.snapshot.Results), counts["GOOD"], counts["BAD"], counts["UNCERTAIN"], counts["REQUEST_FAILED"], counts["PARSE_FAILED"]+counts["TYPE_MISMATCH"], counts["NOT_READ"]))
}

func (d *Desktop) exportPointsDialog() {
	// The dialog can remain open while polling; export exactly this snapshot.
	snapshot := d.snapshot
	filter := d.resultFilter.Selected
	count := 0
	for _, r := range snapshot.Results {
		if matchesPoint(r, filter) {
			count++
		}
	}
	if count == 0 {
		d.showError(errors.New("当前筛选没有可导出的点位"))
		return
	}
	save := dialog.NewFileSave(func(out fyne.URIWriteCloser, err error) {
		if err != nil {
			d.showError(err)
			return
		}
		if out == nil {
			return
		}
		writeErr := exportPointsCSV(out, snapshot, filter)
		closeErr := out.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			d.showError(err)
			return
		}
		dialog.ShowInformation("导出完成", fmt.Sprintf("已导出 %d 行（%s）。", count, filter), d.win)
	}, d.win)
	save.SetTitleText(fmt.Sprintf("导出 CSV · %s · %d 行", filter, count))
	save.SetFileName("opcua-points-" + time.Now().Format("20060102-150405") + ".csv")
	save.Show()
}

func (d *Desktop) showPointHelp() {
	text := widget.NewRichTextFromMarkdown(help.Points)
	text.Wrapping = fyne.TextWrapWord
	box := dialog.NewCustom("点位读取帮助", "关闭", container.NewVScroll(text), d.win)
	box.Resize(fyne.NewSize(760, 550))
	box.Show()
}
