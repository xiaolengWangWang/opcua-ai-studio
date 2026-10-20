package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/viscada"
)

func firstLines(lines []string) string {
	if len(lines) > 5 {
		return strings.Join(lines[:5], "\n") + fmt.Sprintf("\n…等 %d 行", len(lines))
	}
	return strings.Join(lines, "\n")
}

// importAttributesDialog validates the points of a viSCADA attribute sheet:
// each 属性标识 is read like a full read, then results filter by GOOD, BAD,
// 不存在 or 可写, and control points can be written through the write row.
func (d *Desktop) importAttributesDialog() {
	if d.snapshot.State != "connected" {
		d.showError(errors.New("请先连接"))
		return
	}
	open := dialog.NewFileOpen(func(in fyne.URIReadCloser, err error) {
		if err != nil {
			d.showError(err)
			return
		}
		if in == nil {
			return
		}
		name := in.URI().Name()
		started := d.runFile("导入属性表 "+name, func(ctx context.Context, report func(int, int)) (string, error) {
			data, err := io.ReadAll(in)
			in.Close()
			if err != nil {
				return "", err
			}
			attrs, err := viscada.ReadAttributes(ctx, bytes.NewReader(data), int64(len(data)), func(read, total int64) { report(int(read>>10), int(total>>10)) })
			if err != nil {
				return "", err
			}
			var points []service.Point
			var names, invalid []string
			for _, a := range attrs {
				addr, err := viscada.ParseIdentifier(a.Identifier)
				if err != nil {
					invalid = append(invalid, fmt.Sprintf("第 %d 行：%v", a.Row, err))
					continue
				}
				points = append(points, service.Point{Address: addr.NodeID, Type: addr.Type, Count: addr.Count})
				names = append(names, a.Name)
			}
			if len(points) == 0 {
				return "", fmt.Errorf("没有可验证的属性标识\n%s", firstLines(invalid))
			}
			fyne.Do(func() {
				d.jobFollow = true
				d.jobBar.SetValue(0)
				d.jobLabel.SetText(fmt.Sprintf("正在验证 %d 个点位，可随时取消", len(points)))
				d.tabs.SelectIndex(0)
			})
			err = d.controller.ValidatePoints(ctx, name, points, names)
			message := fmt.Sprintf("已验证 %s 的 %d 个点位，可按 GOOD / BAD / 不存在 / 可写筛选；选中控制点位后可写入验证。", name, len(points))
			if len(invalid) > 0 {
				message += fmt.Sprintf("\n跳过 %d 行无效属性标识：\n%s", len(invalid), firstLines(invalid))
			}
			return message, err
		})
		if !started {
			in.Close()
		}
	}, d.win)
	open.SetFilter(storage.NewExtensionFileFilter([]string{".xlsx"}))
	open.Show()
}

// exportAttributesDialog offers the exports of the current snapshot: the rows
// shown, then all, GOOD, control and Chinese points, which a search narrows.
func (d *Desktop) exportAttributesDialog() {
	snapshot := d.snapshot
	q := d.query()
	shown := q.matcher()
	searched := pointQuery{filter: "全部", search: q.search, exact: q.exact}.matcher()
	// count reports the points an option holds and how many of them the
	// platform cannot store, so the choice shows what will be skipped.
	count := func(keep func(*device.ReadResult) bool) (n, skip int) {
		for i := range snapshot.Results {
			if r := &snapshot.Results[i]; keep(r) {
				n++
				if _, err := viscada.FromResult(*r); err != nil {
					skip++
				}
			}
		}
		return n, skip
	}
	var box dialog.Dialog
	choice := func(label, tag string, keep func(*device.ReadResult) bool) *widget.Button {
		if q.search != "" && tag != "当前列表" {
			only := keep
			keep = func(r *device.ReadResult) bool { return searched(r) && only(r) }
		}
		n, skip := count(keep)
		text := fmt.Sprintf("%s（%d 个）", label, n)
		if skip > 0 {
			text = fmt.Sprintf("%s（%d 个，跳过 %d 个）", label, n, skip)
		}
		b := widget.NewButton(text, func() {
			box.Hide()
			d.exportAttributes(snapshot, label, tag, keep, n)
		})
		if n == skip {
			b.Disable()
		}
		return b
	}
	note := "导出 viSCADA 设备类属性表：填写属性标识、属性名称（显示名称）、读写模式和数据类型，其他列留空。\n数组和平台不支持的类型（如 DateTime）会跳过，完成后列出原因。"
	if q.search != "" {
		note += fmt.Sprintf("\n当前搜索“%s”，以下各项只包含搜索到的点位。", q.search)
	}
	content := container.NewVBox(widget.NewLabel(note))
	if q.search != "" || q.filter != "全部" {
		current := choice("当前列表："+q.label(), "当前列表", shown)
		current.Importance = widget.HighImportance
		content.Add(current)
	}
	content.Add(choice("全部点位", "全部", func(*device.ReadResult) bool { return true }))
	content.Add(choice("仅 GOOD 点位", "GOOD", func(r *device.ReadResult) bool { return r.Quality == "GOOD" }))
	content.Add(choice("仅控制点位（可写）", "控制点位", func(r *device.ReadResult) bool { return r.Writable() }))
	content.Add(choice("仅含中文点位", "含中文", func(r *device.ReadResult) bool { return device.HasChinese(r.Address.NodeID) }))
	box = dialog.NewCustom("导出属性表", "取消", content, d.win)
	box.Show()
}

func (d *Desktop) exportAttributes(snapshot service.Snapshot, label, tag string, keep func(*device.ReadResult) bool, n int) {
	name := fmt.Sprintf("viSCADA属性表-%s-%s.xlsx", tag, time.Now().Format("20060102-150405"))
	d.saveFile("导出属性表 · "+label, name, func(ctx context.Context, out io.Writer, report func(int, int)) (string, error) {
		rows := make([]viscada.Attribute, 0, n)
		skipped := map[string]int{}
		long, chinese := 0, 0
		for i := range snapshot.Results {
			r := &snapshot.Results[i]
			if !keep(r) {
				continue
			}
			a, err := viscada.FromResult(*r)
			if err != nil {
				skipped[err.Error()]++
				continue
			}
			if len([]rune(a.Identifier)) > 60 {
				long++
			}
			if device.HasChinese(a.Identifier) {
				chinese++
			}
			rows = append(rows, a)
		}
		var reasons []string
		for reason, k := range skipped {
			reasons = append(reasons, fmt.Sprintf("%s %d 个", reason, k))
		}
		sort.Strings(reasons)
		if len(rows) == 0 {
			return "", errors.New("没有可导出的属性：" + strings.Join(reasons, "；"))
		}
		if err := viscada.WriteAttributes(ctx, out, rows, func(done int) { report(done, len(rows)) }); err != nil {
			return "", err
		}
		message := fmt.Sprintf("已导出 %d 个属性（%s）。", len(rows), label)
		if chinese > 0 && chinese < len(rows) {
			message += fmt.Sprintf("其中 %d 个属性标识含中文。", chinese)
		}
		if len(reasons) > 0 {
			message += "\n跳过：" + strings.Join(reasons, "；")
		}
		if long > 0 {
			message += fmt.Sprintf("\n注意：%d 个属性标识超过 60 个字符，平台可能拒绝导入。", long)
		}
		return message, nil
	})
}
