package ui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
)

// viewModes are the ways to show the results: the table, the browse tree,
// one card per point with its value large, and one chart per numeric point
// of its values over the chosen time range.
var viewModes = []string{"表格", "树状", "卡片", "趋势"}

// Trends sample up to historyPoints numeric points, the shown ones first,
// each time they are read; the samples go to the record database, and a
// chart keeps at most trendPoints of them, thinning older ones.
const (
	historyPoints = 1000
	trendPoints   = 600
)

// trendRanges are the time ranges a trend chart shows.
var trendRanges = []string{"10 分钟", "1 小时", "8 小时", "24 小时"}

func (d *Desktop) trendSpan() time.Duration {
	return map[string]time.Duration{"10 分钟": 10 * time.Minute, "1 小时": time.Hour, "8 小时": 8 * time.Hour, "24 小时": 24 * time.Hour}[d.trendRange.Selected]
}

// resetTrends forgets the charts' samples, for another server or range;
// they are read again from the record database.
func (d *Desktop) resetTrends() {
	d.trend, d.trendLoaded, d.trendWanted = map[string][]service.Sample{}, map[string]bool{}, map[string]bool{}
	d.trendGen++
}

// recordHistory takes a sample of each numeric point read anew when the
// result list changes: the shown points first, up to historyPoints. A point
// whose read time did not change, as when another point was verified, adds
// none.
func (d *Desktop) recordHistory(rr []device.ReadResult) {
	if len(rr) == 0 || d.historyFirst == &rr[0] {
		return
	}
	d.historyFirst = &rr[0]
	if d.trend == nil {
		d.resetTrends()
	}
	if d.trendSampled == nil {
		d.trendSampled = map[string]time.Time{}
	}
	now := time.Now()
	values := map[string]service.Sample{}
	taken, shown := 0, make([]bool, len(rr))
	sample := func(i int) {
		r := &rr[i]
		v, ok := numericValue(r.Value)
		if !ok || r.Quality == "BAD" || taken >= historyPoints {
			return
		}
		taken++
		id, at := r.Address.NodeID, r.Time
		if at.IsZero() {
			at = now
		} else if last, ok := d.trendSampled[id]; ok && !at.After(last) {
			return
		}
		d.trendSampled[id] = at
		s := service.Sample{Time: at, Value: v}
		values[id] = s
		d.trend[id] = append(d.trend[id], s)
	}
	for _, i := range d.visible {
		shown[i] = true
		sample(i)
	}
	for i := range rr {
		if !shown[i] {
			sample(i)
		}
	}
	d.controller.RecordSamples(values)
	since := now.Add(-d.trendSpan())
	for id, ss := range d.trend {
		d.trend[id] = thin(dropBefore(ss, since), trendPoints)
	}
}

func dropBefore(ss []service.Sample, since time.Time) []service.Sample {
	i, _ := slices.BinarySearchFunc(ss, since, func(s service.Sample, t time.Time) int { return s.Time.Compare(t) })
	return ss[i:]
}

// thin keeps at most limit samples: when there are more, each bucket of the
// older samples keeps its lowest and highest, so peaks stay visible; the
// newest quarter is kept whole.
func thin(ss []service.Sample, limit int) []service.Sample {
	if len(ss) <= limit {
		return ss
	}
	keep := limit / 4
	old, recent := ss[:len(ss)-keep], ss[len(ss)-keep:]
	buckets := (limit - keep) / 2
	out := make([]service.Sample, 0, limit)
	for b := range buckets {
		part := old[b*len(old)/buckets : (b+1)*len(old)/buckets]
		if len(part) == 0 {
			continue
		}
		lo, hi := 0, 0
		for i, s := range part {
			if s.Value < part[lo].Value {
				lo = i
			}
			if s.Value > part[hi].Value {
				hi = i
			}
		}
		out = append(out, part[min(lo, hi)])
		if lo != hi {
			out = append(out, part[max(lo, hi)])
		}
	}
	return append(out, recent...)
}

// wantTrend asks for a chart's samples from the record database once it
// shows: a day of a thousand points is too much to read at once.
func (d *Desktop) wantTrend(id string) {
	if d.trendLoaded != nil && !d.trendLoaded[id] {
		d.trendWanted[id] = true
		d.loadTrends()
	}
}

// loadTrends reads the samples of the charts asked for from the record
// database, in the background; samples taken meanwhile are kept.
func (d *Desktop) loadTrends() {
	if d.trendLoading || len(d.trendWanted) == 0 {
		return
	}
	var nodes []string
	for id := range d.trendWanted {
		if len(nodes) == 100 {
			break
		}
		nodes = append(nodes, id)
		delete(d.trendWanted, id)
		d.trendLoaded[id] = true
	}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.workers.Add(1)
	d.mu.Unlock()
	d.trendLoading = true
	gen, since := d.trendGen, time.Now().Add(-d.trendSpan())
	go func() {
		defer d.workers.Done()
		h, err := d.controller.History(nodes, since)
		fyne.Do(func() {
			d.trendLoading = false
			if d.ctx.Err() != nil {
				return
			}
			if gen == d.trendGen && err == nil {
				for _, id := range nodes {
					stored := h[id]
					if len(stored) > 0 {
						newer := dropBefore(d.trend[id], stored[len(stored)-1].Time.Add(time.Millisecond))
						d.trend[id] = thin(append(stored, newer...), trendPoints)
					}
				}
				d.trends.Refresh()
			}
			d.loadTrends()
		})
	}()
}

// card is the frame of a card or chart: a rounded panel.
func card(min fyne.Size, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 6
	bg.StrokeWidth = 1
	bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	bg.SetMinSize(min)
	return container.NewStack(bg, container.NewPadded(content))
}

// newCardView shows one card per visible point: name, value colored by
// quality, then type, access, quality and time; a control point has a
// write button.
func (d *Desktop) newCardView() *widget.GridWrap {
	g := widget.NewGridWrap(func() int { return len(d.visible) }, func() fyne.CanvasObject {
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		value := widget.NewLabel("")
		value.SizeName = theme.SizeNameSubHeadingText
		detail := widget.NewLabel("")
		detail.SizeName = theme.SizeNameCaptionText
		for _, l := range []*widget.Label{name, value, detail} {
			l.Truncation = fyne.TextTruncateEllipsis
		}
		write := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), nil)
		write.Importance = widget.LowImportance
		return card(fyne.NewSize(240, 100), container.NewVBox(container.NewBorder(nil, nil, nil, write, name), value, detail))
	}, func(id widget.GridWrapItemID, o fyne.CanvasObject) {
		if id >= len(d.visible) {
			return
		}
		r := &d.snapshot.Results[d.visible[id]]
		row := pointRow(r)
		box := o.(*fyne.Container).Objects[1].(*fyne.Container).Objects[0].(*fyne.Container).Objects
		header := box[0].(*fyne.Container).Objects
		name, write := header[0].(*widget.Label), header[1].(*widget.Button)
		value, detail := box[1].(*widget.Label), box[2].(*widget.Label)
		name.SetText(or(r.DisplayName, r.Address.NodeID))
		value.Importance = qualityImportance(r.Quality)
		if t, ok := d.changedAt[r.Address.NodeID]; ok && time.Since(t) < changeHighlight {
			value.Importance = widget.HighImportance
		}
		value.SetText(or(row[4], r.Quality))
		detail.SetText(strings.Join(nonEmpty(row[2], row[3], row[5], row[6]), " · "))
		node := r.Address.NodeID
		write.OnTapped = func() { d.writeNode(node) }
		if r.Writable() && d.snapshot.State == "connected" {
			write.Show()
		} else {
			write.Hide()
		}
	})
	g.OnSelected = func(id widget.GridWrapItemID) {
		if id < len(d.visible) {
			d.selectResult(d.snapshot.Results[d.visible[id]])
		}
		g.UnselectAll()
	}
	return g
}

// newTrendView shows one chart per visible numeric point over the chosen
// range, each scaled to its own values; clicking one shows it large.
func (d *Desktop) newTrendView() *widget.GridWrap {
	g := widget.NewGridWrap(func() int { return len(d.trendRows) }, func() fyne.CanvasObject {
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		name.Truncation = fyne.TextTruncateEllipsis
		detail := widget.NewLabel("")
		detail.SizeName = theme.SizeNameCaptionText
		detail.Truncation = fyne.TextTruncateEllipsis
		return card(fyne.NewSize(320, 160), container.NewBorder(name, detail, nil, nil, newSparkline()))
	}, func(id widget.GridWrapItemID, o fyne.CanvasObject) {
		if id >= len(d.trendRows) {
			return
		}
		r := &d.snapshot.Results[d.trendRows[id]]
		parts := o.(*fyne.Container).Objects[1].(*fyne.Container).Objects[0].(*fyne.Container).Objects
		line, name, detail := parts[0].(*sparkline), parts[1].(*widget.Label), parts[2].(*widget.Label)
		ss := d.trend[r.Address.NodeID]
		d.wantTrend(r.Address.NodeID)
		name.SetText(fmt.Sprintf("%s = %s", or(r.DisplayName, r.Address.NodeID), or(pointRow(r)[4], r.Quality)))
		detail.SetText(trendSummary(d.trendRange.Selected, ss))
		line.color = theme.Color(theme.ColorNamePrimary)
		if r.Quality == "BAD" {
			line.color = theme.Color(theme.ColorNameError)
		}
		line.points, line.to = ss, time.Now()
		line.from = line.to.Add(-d.trendSpan())
		line.Refresh()
	})
	g.OnSelected = func(id widget.GridWrapItemID) {
		if id < len(d.trendRows) {
			r := d.snapshot.Results[d.trendRows[id]]
			d.selectResult(r)
			d.wantTrend(r.Address.NodeID)
			d.trendDialog(r)
		}
		g.UnselectAll()
	}
	return g
}

func trendSummary(span string, ss []service.Sample) string {
	if len(ss) < 2 {
		return "最近 " + span + "暂无记录：勾选“自动刷新”后开始记录"
	}
	lo, hi := ss[0].Value, ss[0].Value
	for _, s := range ss {
		lo, hi = min(lo, s.Value), max(hi, s.Value)
	}
	return fmt.Sprintf("最近 %s · %d 个样本 · 最小 %s · 最大 %s", span, len(ss), formatNumber(lo), formatNumber(hi))
}

// trendDialog shows one point's chart large, with its range, statistics
// and the samples to copy.
func (d *Desktop) trendDialog(r device.ReadResult) {
	ss := slices.Clone(d.trend[r.Address.NodeID])
	line := newSparkline()
	line.points, line.to = ss, time.Now()
	line.from = line.to.Add(-d.trendSpan())
	axis := func(text string, align fyne.TextAlign) *widget.Label {
		l := widget.NewLabelWithStyle(text, align, fyne.TextStyle{})
		l.SizeName = theme.SizeNameCaptionText
		return l
	}
	stats := "暂无记录：勾选“自动刷新”后开始记录"
	top, bottom := "", ""
	if len(ss) > 0 {
		lo, hi, sum := ss[0].Value, ss[0].Value, 0.0
		for _, s := range ss {
			lo, hi, sum = min(lo, s.Value), max(hi, s.Value), sum+s.Value
		}
		top, bottom = "最大 "+formatNumber(hi), "最小 "+formatNumber(lo)
		stats = fmt.Sprintf("%d 个样本 · 最小 %s · 最大 %s · 平均 %s · 最新 %s（%s）", len(ss), formatNumber(lo), formatNumber(hi), formatNumber(sum/float64(len(ss))),
			formatNumber(ss[len(ss)-1].Value), ss[len(ss)-1].Time.Local().Format("15:04:05"))
	}
	times := container.NewBorder(nil, nil, axis(line.from.Local().Format("01-02 15:04:05"), fyne.TextAlignLeading), axis(line.to.Local().Format("01-02 15:04:05"), fyne.TextAlignTrailing))
	chart := card(fyne.NewSize(760, 320), container.NewBorder(axis(top, fyne.TextAlignLeading), axis(bottom, fyne.TextAlignLeading), nil, nil, line))
	copyData := widget.NewButtonWithIcon("复制数据", theme.ContentCopyIcon(), func() {
		lines := []string{"时间\t值"}
		for _, s := range ss {
			lines = append(lines, s.Time.Local().Format("2006-01-02 15:04:05.000")+"\t"+formatNumber(s.Value))
		}
		d.win.Clipboard().SetContent(strings.Join(lines, "\n"))
	})
	head := widget.NewLabel(fmt.Sprintf("%s · 最近 %s\n%s", r.Address.NodeID, d.trendRange.Selected, stats))
	head.Wrapping = fyne.TextWrapWord
	box := dialog.NewCustom("趋势 · "+or(r.DisplayName, r.Address.NodeID), "关闭", container.NewBorder(head, container.NewBorder(nil, nil, nil, copyData, times), nil, nil, chart), d.win)
	box.Resize(fyne.NewSize(820, 500))
	box.Show()
}

// refreshViews updates the card and trend views when they show.
func (d *Desktop) refreshViews() {
	switch d.viewMode.Selected {
	case "卡片":
		d.cards.Refresh()
	case "趋势":
		d.trendRows = d.trendRows[:0]
		for _, i := range d.visible {
			if _, ok := numericValue(d.snapshot.Results[i].Value); ok || len(d.trend[d.snapshot.Results[i].Address.NodeID]) > 0 {
				d.trendRows = append(d.trendRows, i)
			}
		}
		d.trends.Refresh()
	}
}

func formatNumber(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

// sparkline draws samples as connected segments over a time axis, scaled to
// their own range; a long pause, such as the program closed, leaves a gap.
type sparkline struct {
	widget.BaseWidget
	points   []service.Sample
	from, to time.Time // the time axis; zero spreads the samples evenly
	color    color.Color
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
	v, pad := r.s.points, float32(6)
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
	lo, hi := v[0].Value, v[0].Value
	for _, x := range v {
		lo, hi = min(lo, x.Value), max(hi, x.Value)
	}
	if hi == lo {
		hi, lo = hi+1, lo-1
	}
	span := r.s.to.Sub(r.s.from)
	x := func(i int) float32 {
		if span <= 0 {
			return float32(i) / float32(n)
		}
		return float32(min(max(v[i].Time.Sub(r.s.from).Seconds()/span.Seconds(), 0), 1))
	}
	at := func(i int) fyne.Position {
		return fyne.NewPos(pad+w*x(i), pad+h-h*float32((v[i].Value-lo)/(hi-lo)))
	}
	// A step far longer than the usual one is a pause in the sampling.
	gap := time.Duration(0)
	if steps := v[n].Time.Sub(v[0].Time) / time.Duration(n); steps > 0 {
		gap = max(10*steps, time.Minute)
	}
	for i, l := range r.lines {
		l.StrokeColor = r.s.color
		l.Position1, l.Position2 = at(i), at(i+1)
		if gap > 0 && v[i+1].Time.Sub(v[i].Time) > gap {
			l.Hide()
		} else {
			l.Show()
		}
	}
}
