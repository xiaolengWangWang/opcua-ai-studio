package ui

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
)

// writeForm is the write dialog's content for one point: its type, the new
// value or true / false for a Boolean, and its own 允许写入 that every write
// clears again.
type writeForm struct {
	node            string
	count           int
	types           *widget.Select
	value           *widget.Entry
	choice          *widget.RadioGroup
	allow           *widget.Check
	write           *widget.Button
	current, status *widget.Label
}

func (d *Desktop) newWriteForm(r device.ReadResult) *writeForm {
	f := &writeForm{node: r.Address.NodeID, count: 1}
	if r.Value.Elements != nil {
		f.count = max(1, len(r.Value.Elements))
	} else if r.Address.Count > 1 {
		f.count = r.Address.Count
	}
	kind := string(r.Kind())
	if kind == "" && r.Address.Type != "Auto" {
		kind = string(r.Address.Type)
	}
	f.types = widget.NewSelect(device.Types[1:], nil)
	f.types.PlaceHolder = "请选择类型"
	f.value = widget.NewEntry()
	f.value.SetPlaceHolder("标量或 JSON 数组，例如 12.5、中文文本、[1,2,3]")
	f.current = widget.NewLabelWithStyle("未读取", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	if r.Value.Type != "" {
		f.current.SetText(device.FormatValue(r.Value))
		if r.Value.Elements == nil {
			f.value.SetText(f.current.Text)
		}
	}
	f.choice = widget.NewRadioGroup([]string{"true", "false"}, func(v string) {
		if v != "" {
			f.value.SetText(v)
		}
	})
	f.choice.Horizontal = true
	f.types.OnChanged = func(kind string) {
		if kind != "Boolean" {
			f.choice.Hide()
			f.value.Show()
			return
		}
		f.value.Hide()
		f.choice.Show()
		if v := f.value.Text; v == "true" || v == "false" {
			f.choice.SetSelected(v)
		} else {
			f.choice.SetSelected("")
		}
	}
	f.types.SetSelected(kind)
	f.types.OnChanged(kind)
	f.status = widget.NewLabel("")
	f.status.Wrapping = fyne.TextWrapWord
	f.write = widget.NewButtonWithIcon("写入并读回", theme.ConfirmIcon(), func() { d.submitWrite(f) })
	f.write.Importance = widget.DangerImportance
	f.write.Disable()
	f.allow = widget.NewCheck("允许写入（写入会改变现场设备的设定值或控制状态）", func(on bool) {
		if on {
			f.write.Enable()
		} else {
			f.write.Disable()
		}
	})
	return f
}

// submitWrite writes the form's value, reads it back and shows the result
// and the new current value; the form stays open for another write.
func (d *Desktop) submitWrite(f *writeForm) {
	if f.types.Selected == "" {
		f.status.SetText("请先选择类型")
		return
	}
	a := device.Address{NodeID: f.node, Type: device.DataType(f.types.Selected), Count: f.count}
	if _, err := device.ParseValue(a, f.value.Text); err != nil {
		f.status.SetText(err.Error())
		return
	}
	req := service.WriteRequest{Point: service.Point{Address: a.NodeID, Type: a.Type, Count: a.Count}, Value: f.value.Text, Generation: d.controller.Snapshot().Generation}
	f.allow.SetChecked(false)
	f.status.SetText("正在写入……")
	var werr error
	d.run(func(ctx context.Context) error { werr = d.controller.Write(ctx, req, true); return nil }, func() {
		s := d.controller.Snapshot()
		switch {
		case werr != nil:
			f.status.SetText("写入失败：" + werr.Error())
		case s.WriteResult != nil:
			f.status.SetText(fmt.Sprintf("写入：%s · 读回%s", s.WriteResult.State, s.Readback))
		}
		for i := range s.Results {
			if r := &s.Results[i]; r.Address.NodeID == f.node && r.Value.Type != "" {
				f.current.SetText(device.FormatValue(r.Value))
			}
		}
	})
}

// writeDialog writes one point directly: its current value is shown and
// prefilled, and the write needs the dialog's own 允许写入, then is read back.
func (d *Desktop) writeDialog(r device.ReadResult) {
	if d.controller.Snapshot().State != "connected" {
		d.showError(errors.New("请先连接"))
		return
	}
	f := d.newWriteForm(r)
	form := widget.NewForm(
		widget.NewFormItem("Endpoint", widget.NewLabel(d.snapshot.Target)),
		widget.NewFormItem("NodeID", widget.NewLabel(r.Address.NodeID)),
		widget.NewFormItem("名称", widget.NewLabel(or(r.DisplayName, "—"))),
		widget.NewFormItem("类型", f.types),
		widget.NewFormItem("当前值", f.current),
		widget.NewFormItem("写入值", container.NewVBox(f.value, f.choice)),
	)
	box := dialog.NewCustom("写入点位", "关闭", container.NewVBox(form, f.allow, f.write, f.status), d.win)
	box.Resize(fyne.NewSize(580, 0))
	box.Show()
	d.win.Canvas().Focus(f.value)
}

// writeNode opens the write dialog for a point of the results.
func (d *Desktop) writeNode(node string) {
	for i := range d.snapshot.Results {
		if d.snapshot.Results[i].Address.NodeID == node {
			d.writeDialog(d.snapshot.Results[i])
			return
		}
	}
}

// writeSingle opens the write dialog for the single point card's point,
// with its type and count when it has not been read.
func (d *Desktop) writeSingle() {
	p, _, err := d.singlePoint()
	if err != nil {
		d.showError(err)
		return
	}
	for i := range d.snapshot.Results {
		if d.snapshot.Results[i].Address.NodeID == p.Address {
			d.writeDialog(d.snapshot.Results[i])
			return
		}
	}
	d.writeDialog(device.ReadResult{Address: device.Address{NodeID: p.Address, Type: p.Type, Count: p.Count}})
}
