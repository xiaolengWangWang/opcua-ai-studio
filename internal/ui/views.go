package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
)

// viewModes are the ways to show the results: the table, the browse tree,
// one card per point with its value large, and one small chart per numeric
// point of its recent values.
var viewModes = []string{"表格", "树状", "卡片", "趋势"}

// History keeps the last historyLen values of up to historyPoints numeric
// points, a sample each time the results change, as auto refresh does.
const (
	historyLen    = 120
	historyPoints = 1000
)

// recordHistory adds a sample per numeric point when the result list is new.
func (d *Desktop) recordHistory(rr []device.ReadResult) {
	if len(rr) == 0 || d.historyFirst == &rr[0] {
		return
	}
	d.historyFirst = &rr[0]
	if d.history == nil {
		d.history = map[string][]float64{}
	}
	kept := 0
	for i := range rr {
		v, ok := numericValue(rr[i].Value)
		if !ok || rr[i].Quality == "BAD" {
			continue
		}
		if kept++; kept > historyPoints {
			break
		}
		id := rr[i].Address.NodeID
		h := append(d.history[id], v)
		if len(h) > historyLen {
			h = h[len(h)-historyLen:]
		}
		d.history[id] = h
	}
}

// newCardView shows one card per visible point: name, value colored by
// quality, then type, access, quality and time.
func (d *Desktop) newCardView() *widget.GridWrap {
	g := widget.NewGridWrap(func() int { return len(d.visible) }, func() fyne.CanvasObject {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		bg.CornerRadius = 6
		bg.StrokeWidth = 1
		bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		bg.SetMinSize(fyne.NewSize(230, 96))
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		value := widget.NewLabel("")
		value.SizeName = theme.SizeNameSubHeadingText
		detail := widget.NewLabel("")
		detail.SizeName = theme.SizeNameCaptionText
		for _, l := range []*widget.Label{name, value, detail} {
			l.Truncation = fyne.TextTruncateEllipsis
		}
		return container.NewStack(bg, container.NewVBox(name, value, detail))
	}, func(id widget.GridWrapItemID, o fyne.CanvasObject) {
		if id >= len(d.visible) {
			return
		}
		r := &d.snapshot.Results[d.visible[id]]
		row := pointRow(r)
		labels := o.(*fyne.Container).Objects[1].(*fyne.Container).Objects
		name, value, detail := labels[0].(*widget.Label), labels[1].(*widget.Label), labels[2].(*widget.Label)
		name.SetText(or(r.DisplayName, r.Address.NodeID))
		value.Importance = qualityImportance(r.Quality)
		if t, ok := d.changedAt[r.Address.NodeID]; ok && time.Since(t) < changeHighlight {
			value.Importance = widget.HighImportance
		}
		value.SetText(or(row[4], r.Quality))
		detail.SetText(strings.Join(nonEmpty(row[2], row[3], row[5], row[6]), " · "))
	})
	g.OnSelected = func(id widget.GridWrapItemID) {
		if id < len(d.visible) {
			d.selectResult(d.snapshot.Results[d.visible[id]])
		}
		g.UnselectAll()
	}
	return g
}

// newTrendView shows one small chart per visible numeric point, each scaled
// to its own range, with its latest value and range.
func (d *Desktop) newTrendView() *widget.GridWrap {
	g := widget.NewGridWrap(func() int { return len(d.trendRows) }, func() fyne.CanvasObject {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		bg.CornerRadius = 6
		bg.StrokeWidth = 1
		bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		bg.SetMinSize(fyne.NewSize(300, 150))
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		name.Truncation = fyne.TextTruncateEllipsis
		detail := widget.NewLabel("")
		detail.SizeName = theme.SizeNameCaptionText
		detail.Truncation = fyne.TextTruncateEllipsis
		return container.NewStack(bg, container.NewBorder(name, detail, nil, nil, newSparkline()))
	}, func(id widget.GridWrapItemID, o fyne.CanvasObject) {
		if id >= len(d.trendRows) {
			return
		}
		r := &d.snapshot.Results[d.trendRows[id]]
		parts := o.(*fyne.Container).Objects[1].(*fyne.Container).Objects
		var name, detail *widget.Label
		var line *sparkline
		for _, p := range parts {
			switch w := p.(type) {
			case *sparkline:
				line = w
			case *widget.Label:
				if w.TextStyle.Bold {
					name = w
				} else {
					detail = w
				}
			}
		}
		h := d.history[r.Address.NodeID]
		name.SetText(fmt.Sprintf("%s = %s", or(r.DisplayName, r.Address.NodeID), or(pointRow(r)[4], r.Quality)))
		if len(h) < 2 {
			detail.SetText("勾选“自动刷新”后开始记录趋势")
		} else {
			lo, hi := h[0], h[0]
			for _, v := range h {
				lo, hi = min(lo, v), max(hi, v)
			}
			detail.SetText(fmt.Sprintf("最近 %d 次 · 最小 %s · 最大 %s", len(h), formatNumber(lo), formatNumber(hi)))
		}
		line.color = theme.Color(theme.ColorNamePrimary)
		if r.Quality == "BAD" {
			line.color = theme.Color(theme.ColorNameError)
		}
		line.values = h
		line.Refresh()
	})
	g.OnSelected = func(id widget.GridWrapItemID) {
		if id < len(d.trendRows) {
			d.selectResult(d.snapshot.Results[d.trendRows[id]])
		}
		g.UnselectAll()
	}
	return g
}

// refreshViews updates the card and trend views when they show.
func (d *Desktop) refreshViews() {
	switch d.viewMode.Selected {
	case "卡片":
		d.cards.Refresh()
	case "趋势":
		d.trendRows = d.trendRows[:0]
		for _, i := range d.visible {
			if _, ok := numericValue(d.snapshot.Results[i].Value); ok || len(d.history[d.snapshot.Results[i].Address.NodeID]) > 0 {
				d.trendRows = append(d.trendRows, i)
			}
		}
		d.trends.Refresh()
	}
}

func formatNumber(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

// sparkline draws a series as connected segments scaled to its own range.
type sparkline struct {
	widget.BaseWidget
	values []float64
	color  color.Color
}

func newSparkline() *sparkline {
	s := &sparkline{color: theme.Color(theme.ColorNamePrimary)}
	s.ExtendBaseWidget(s)
	return s
}

func (s *sparkline) CreateRenderer() fyne.WidgetRenderer {
	axis := canvas.NewLine(theme.Color(theme.ColorNameSeparator))
	return &sparkRenderer{s: s, axis: axis}
}

type sparkRenderer struct {
	s     *sparkline
	axis  *canvas.Line
	lines []*canvas.Line
	size  fyne.Size
}

func (r *sparkRenderer) Layout(size fyne.Size) { r.size = size; r.place() }
func (r *sparkRenderer) MinSize() fyne.Size    { return fyne.NewSize(120, 60) }
func (r *sparkRenderer) Refresh()              { r.place(); canvas.Refresh(r.s) }
func (r *sparkRenderer) Destroy()              {}

func (r *sparkRenderer) Objects() []fyne.CanvasObject {
	out := []fyne.CanvasObject{r.axis}
	for _, l := range r.lines {
		out = append(out, l)
	}
	return out
}

// place positions one segment per pair of samples, with a margin so the
// line stays inside the card.
func (r *sparkRenderer) place() {
	v, pad := r.s.values, float32(6)
	w, h := r.size.Width-2*pad, r.size.Height-2*pad
	r.axis.Position1, r.axis.Position2 = fyne.NewPos(pad, pad+h), fyne.NewPos(pad+w, pad+h)
	n := max(len(v)-1, 0)
	for len(r.lines) < n {
		l := canvas.NewLine(r.s.color)
		l.StrokeWidth = 1.5
		r.lines = append(r.lines, l)
	}
	r.lines = r.lines[:n]
	if n == 0 || w <= 0 || h <= 0 {
		return
	}
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = min(lo, x), max(hi, x)
	}
	if hi == lo {
		hi, lo = hi+1, lo-1
	}
	at := func(i int) fyne.Position {
		return fyne.NewPos(pad+w*float32(i)/float32(n), pad+h-h*float32((v[i]-lo)/(hi-lo)))
	}
	for i, l := range r.lines {
		l.StrokeColor = r.s.color
		l.Position1, l.Position2 = at(i), at(i+1)
	}
}
