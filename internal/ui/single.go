package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/viscada"
)

// singleOptions bounds the node list of the single point card; the search
// narrows the results it is taken from.
const singleOptions = 100

// singlePoint reads the single point card: a NodeID with the card's type and
// count, or a viSCADA 属性标识, returned as identifier, whose data type is
// then shown in the card.
func (d *Desktop) singlePoint() (p service.Point, identifier string, err error) {
	n, err := strconv.Atoi(strings.TrimSpace(d.singleCount.Text))
	if err != nil {
		return p, "", errors.New("元素数量须为整数")
	}
	node := d.singleNode.Text
	if strings.TrimSpace(node) == "" && !strings.Contains(strings.Trim(d.addresses.Text, "\r\n"), "\n") {
		node = strings.Trim(d.addresses.Text, "\r\n")
	}
	a, isID, err := parsePoint(node, device.DataType(d.singleKind.Selected), n)
	if err != nil {
		return p, "", err
	}
	if isID {
		identifier = strings.TrimSpace(node)
		d.singleKind.SetSelected(string(a.Type))
		d.singleCount.SetText("1")
	}
	return service.Point{Address: a.NodeID, Type: a.Type, Count: a.Count}, identifier, nil
}

func (d *Desktop) verifySingle() {
	p, identifier, err := d.singlePoint()
	if err != nil {
		d.showError(err)
		return
	}
	d.runCheck(func(ctx context.Context) error { return d.controller.VerifyPoint(ctx, p) }, func() {
		s := d.controller.Snapshot()
		for i := range s.Results {
			if s.Results[i].Address.NodeID == p.Address {
				d.showSingle(&s.Results[i], identifier)
				return
			}
		}
	})
}

// selectResult takes a table row or tree point into the single point card,
// with its declared type, so an imported or BAD control point can be written.
func (d *Desktop) selectResult(r device.ReadResult) {
	d.selectedNode = r.Address.NodeID
	d.singleNode.SetText(r.Address.NodeID)
	kind := string(r.Kind())
	if kind == "" {
		kind = "Auto"
	}
	d.singleKind.SetSelected(kind)
	n := 1
	if r.Value.Elements != nil {
		n = max(1, len(r.Value.Elements))
	}
	d.singleCount.SetText(strconv.Itoa(n))
	d.showSingle(&r, "")
}

// showSingle describes one point in the card: value, quality, type, access
// and its viSCADA 属性标识. Given an identifier, it checks it against the
// server and proposes the one the server's type implies.
func (d *Desktop) showSingle(r *device.ReadResult, identifier string) {
	row := pointRow(r)
	lines := []string{"NodeID：" + r.Address.NodeID}
	if r.DisplayName != "" {
		lines = append(lines, "名称："+r.DisplayName)
	}
	access := row[3]
	if access == "" {
		access = "读写未知"
	}
	lines = append(lines, fmt.Sprintf("类型：%s · %s", or(row[2], "未知"), access))
	if row[4] != "" {
		lines = append(lines, "值："+row[4])
	}
	lines = append(lines, "质量："+row[5], "执行："+row[7])
	if row[6] != "" {
		lines = append(lines, "时间："+row[6])
	}
	ok := r.Quality == "GOOD" && r.Execution == "READ"
	d.singleIdent = ""
	if identifier != "" {
		problem, want := viscada.Verify(identifier, *r)
		if problem == "" {
			lines = append(lines, "属性标识校验：通过，节点存在且数据类型一致")
		} else {
			ok = false
			lines = append(lines, "属性标识校验：不通过，"+problem)
		}
		if want != "" && want != identifier {
			lines = append(lines, "建议属性标识："+want)
		}
		d.singleIdent = want
	} else if a, err := viscada.FromResult(*r); err == nil {
		lines = append(lines, "viSCADA 属性标识："+a.Identifier)
		d.singleIdent = a.Identifier
	} else {
		lines = append(lines, "viSCADA 属性标识：无（"+err.Error()+"）")
	}
	if d.singleIdent != "" {
		mode := map[bool]string{true: "2 读写", false: "1 只读"}[r.Writable()]
		if r.Access == "" {
			mode = "未知（服务器未报告读写权限）"
		}
		lines = append(lines, "读写模式："+mode)
	}
	d.singleResult.SetText(strings.Join(lines, "\n"))
	d.singleResult.Importance = map[bool]widget.Importance{true: widget.SuccessImportance, false: widget.DangerImportance}[ok]
	d.singleResult.Refresh()
	d.singleResult.Show()
	d.controls()
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// refreshSingleOptions offers the first visible points in the node list of
// the single point card; choosing one also chooses its type.
func (d *Desktop) refreshSingleOptions() {
	options := make([]string, 0, min(len(d.visible), singleOptions))
	kinds := make(map[string]string, cap(options))
	for _, i := range d.visible[:min(len(d.visible), singleOptions)] {
		r := &d.snapshot.Results[i]
		options = append(options, r.Address.NodeID)
		kinds[r.Address.NodeID] = or(string(r.Kind()), "Auto")
	}
	d.singleKinds = kinds
	d.singleNode.SetOptions(options)
}

// referenceKey keys the reference point by endpoint; the demo's port changes
// with every start, so it has one key.
func (d *Desktop) referenceKey(target string) string {
	if s := d.controller.Snapshot(); target != "" && target == s.DemoTarget {
		return demoReference
	}
	return "reference " + target
}

const demoReference = "reference demo"

// loadReference gives the controller the reference point saved under key,
// before connecting, so the connection reads it.
func (d *Desktop) loadReference(key string) {
	d.reference = nil
	var p service.Point
	if text := d.app.Preferences().String(key); text != "" && json.Unmarshal([]byte(text), &p) == nil {
		d.reference = &p
	}
	d.controller.SetReference(d.reference)
	d.refreshReference()
}

// setReference marks the card's point, which must read GOOD, as the point
// that tells whether this server serves data.
func (d *Desktop) setReference() {
	p, _, err := d.singlePoint()
	if err != nil {
		d.showError(err)
		return
	}
	good := false
	for _, r := range d.snapshot.Results {
		if r.Address.NodeID == p.Address {
			good = r.Quality == "GOOD" && r.Execution == "READ"
		}
	}
	if !good {
		d.showError(errors.New("请先单点验证，读数正常（GOOD）的点位才能设为基准点位"))
		return
	}
	b, _ := json.Marshal(p)
	d.app.Preferences().SetString(d.referenceKey(d.snapshot.Target), string(b))
	d.reference = &p
	d.controller.SetReference(&p)
	d.run(d.controller.CheckReference, nil)
}

func (d *Desktop) clearReference() {
	d.app.Preferences().RemoveValue(d.referenceKey(d.snapshot.Target))
	d.reference = nil
	d.controller.SetReference(nil)
	d.refreshReference()
}

// refreshReference shows the reference point's last read in the header.
func (d *Desktop) refreshReference() {
	if d.reference == nil {
		d.refBox.Hide()
		return
	}
	text, importance := "基准点位 "+d.reference.Address+"：未检测", widget.MediumImportance
	if r := d.snapshot.Reference; r != nil && r.Address.NodeID == d.reference.Address {
		row := pointRow(r)
		text = fmt.Sprintf("基准点位 %s：%s", or(r.DisplayName, r.Address.NodeID), r.Quality)
		if row[4] != "" {
			text += " = " + row[4]
		}
		if !r.Time.IsZero() {
			text += " · " + r.Time.Local().Format("15:04:05")
		}
		if r.Error != "" {
			text += " · " + r.Error
		}
		importance = qualityImportance(r.Quality)
		if r.Execution == "REQUEST_FAILED" {
			importance = widget.DangerImportance
		}
	}
	d.refLabel.Importance = importance
	d.refLabel.SetText(text)
	d.refBox.Show()
}

// startRefresh re-reads the shown points at the chosen interval.
func (d *Desktop) startRefresh() {
	interval, err := time.ParseDuration(d.refreshInterval.Selected)
	if err != nil {
		d.showError(err)
		return
	}
	d.run(func(ctx context.Context) error { return d.controller.StartRefresh(ctx, interval) }, nil)
}
