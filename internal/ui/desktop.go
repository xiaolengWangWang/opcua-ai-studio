package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/assets/icon"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/simulator"
	"opcua-ai-studio/platform"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Desktop struct {
	app                                                                                                              fyne.App
	win                                                                                                              fyne.Window
	controller                                                                                                       *service.Controller
	ctx                                                                                                              context.Context
	cancel                                                                                                           context.CancelFunc
	mu                                                                                                               sync.Mutex
	workers                                                                                                          sync.WaitGroup
	once, showOnce                                                                                                   sync.Once
	stopped                                                                                                          bool
	closeErr                                                                                                         error
	busy                                                                                                             int
	revision                                                                                                         uint64
	snapshot                                                                                                         service.Snapshot
	target, timeout, username, password, cert, key, serverCert, addresses, count, interval, writeValue, browseRoot   *widget.Entry
	policy, mode, kind                                                                                               *widget.Select
	allowWrite                                                                                                       *widget.Check
	status, lastError, recordStatus, writeStatus, logs, events                                                       *widget.Label
	activity                                                                                                         *widget.Activity
	connectBtn, disconnectBtn, demoBtn, stopDemoBtn, readBtn, pollBtn, stopPollBtn, writeBtn, browseBtn, discoverBtn *widget.Button
	table, browseTable                                                                                               *widget.Table
	resultFilter                                                                                                     *widget.Select
	singleNode, allRoot                                                                                              *widget.Entry
	verifyBtn, fullBtn, browseFullBtn, stopReadBtn, exportBtn                                                        *widget.Button
	pointSummary, scanStatus                                                                                         *widget.Label
	visible                                                                                                          []int
	pointsKey                                                                                                        pointsKey
	nodesKey                                                                                                         *device.Node
	nodesLen                                                                                                         int
	browseSelection                                                                                                  []device.Node
	browseSelected                                                                                                   map[string]bool
	selectionLabel, jobLabel                                                                                         *widget.Label
	selectAllBtn, clearSelectBtn, readSelectedBtn, importBtn, exportAttrBtn, probeBtn                                *widget.Button
	jobBox                                                                                                           *fyne.Container
	jobBar                                                                                                           *widget.ProgressBar
	jobFollow                                                                                                        bool
	checkCancel                                                                                                      context.CancelFunc
	tabs                                                                                                             *container.AppTabs
}

func NewDesktop(a fyne.App, c *service.Controller, version string) *Desktop {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Desktop{app: a, controller: c, ctx: ctx, cancel: cancel}
	d.win = a.NewWindow("OPC UA AI Studio " + version)
	d.win.SetIcon(icon.Application())
	d.status = widget.NewLabel("未连接")
	d.lastError = widget.NewLabel("")
	d.lastError.Importance = widget.DangerImportance
	d.lastError.Wrapping = fyne.TextWrapWord
	d.lastError.Hide()
	d.activity = widget.NewActivity()
	d.activity.Hide()
	d.recordStatus = widget.NewLabel("")
	d.recordStatus.Wrapping = fyne.TextWrapBreak
	header := container.NewBorder(nil, nil, widget.NewLabelWithStyle("OPC UA AI Studio  "+version, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(d.activity, d.status))
	d.tabs = container.NewAppTabs(container.NewTabItemWithIcon("点位与读写", theme.ListIcon(), d.pointsPanel()), container.NewTabItemWithIcon("节点浏览", theme.SearchIcon(), d.browsePanel()), container.NewTabItemWithIcon("通讯与日志", theme.DocumentIcon(), d.diagnosticsPanel()), container.NewTabItemWithIcon("AI 诊断", theme.InfoIcon(), d.aiPanel()))
	open := widget.NewButtonWithIcon("打开记录目录", theme.FolderOpenIcon(), func() {
		if err := platform.OpenFile(c.Snapshot().RecordDir); err != nil {
			d.showError(err)
		}
	})
	help := widget.NewButtonWithIcon("读取帮助", theme.HelpIcon(), d.showPointHelp)
	footer := container.NewBorder(widget.NewSeparator(), nil, nil, container.NewHBox(help, open), d.recordStatus)
	d.win.SetContent(container.NewPadded(container.NewBorder(container.NewVBox(header, d.connectionPanel(), d.lastError), footer, nil, nil, d.tabs)))
	w, h := platform.WorkArea()
	d.win.Resize(platform.FitSize(fyne.NewSize(1180, 820), w, h, d.win.Canvas().Scale()))
	d.loadInputs()
	d.refresh(c.Snapshot())
	d.win.SetCloseIntercept(func() {
		d.cancel()
		go func() { d.Shutdown(); fyne.Do(func() { d.win.SetCloseIntercept(nil); d.win.Close(); a.Quit() }) }()
	})
	return d
}
func (d *Desktop) Window() fyne.Window { return d.win }
func (d *Desktop) Show() {
	d.showOnce.Do(func() {
		d.workers.Add(1)
		go func() {
			defer d.workers.Done()
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-d.ctx.Done():
					return
				case <-t.C:
					s := d.controller.Snapshot()
					fyne.Do(func() {
						if d.ctx.Err() == nil {
							d.refresh(s)
						}
					})
				}
			}
		}()
	})
	d.win.Show()
}
func (d *Desktop) Shutdown() error {
	d.once.Do(func() {
		d.mu.Lock()
		d.stopped = true
		d.cancel()
		d.mu.Unlock()
		d.closeErr = d.controller.Close()
		d.workers.Wait()
	})
	return d.closeErr
}
func (d *Desktop) run(fn func(context.Context) error, success func()) {
	if d.busy != 0 {
		return
	}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.workers.Add(1)
	d.mu.Unlock()
	d.busy++
	d.controls()
	go func() {
		defer d.workers.Done()
		err := fn(d.ctx)
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			d.busy--
			if d.busy == 0 && d.checkCancel != nil {
				d.checkCancel()
				d.checkCancel = nil
			}
			if d.busy == 0 && d.jobBox != nil {
				d.jobBox.Hide()
				d.jobFollow = false
			}
			if err != nil {
				d.showError(err)
			} else {
				d.lastError.Hide()
				if success != nil {
					success()
				}
			}
			d.refresh(d.controller.Snapshot())
			d.controls()
		})
	}()
}
func (d *Desktop) showError(err error) { d.lastError.SetText(err.Error()); d.lastError.Show() }
func (d *Desktop) controls() {
	connected := d.snapshot.State == "connected"
	idle := d.busy == 0
	set := func(b *widget.Button, on bool) {
		if on {
			b.Enable()
		} else {
			b.Disable()
		}
	}
	set(d.connectBtn, idle)
	set(d.discoverBtn, idle)
	set(d.probeBtn, idle)
	set(d.disconnectBtn, idle && connected)
	set(d.demoBtn, idle && d.snapshot.DemoTarget == "")
	set(d.stopDemoBtn, idle && d.snapshot.DemoTarget != "")
	set(d.readBtn, idle && connected)
	set(d.verifyBtn, idle && connected)
	set(d.fullBtn, idle && connected)
	set(d.browseFullBtn, idle && connected)
	set(d.stopReadBtn, d.checkCancel != nil)
	set(d.exportBtn, idle && len(d.snapshot.Results) > 0)
	set(d.exportAttrBtn, idle && len(d.snapshot.Results) > 0)
	set(d.importBtn, idle && connected)
	set(d.selectAllBtn, len(d.snapshot.Nodes) > 0)
	set(d.clearSelectBtn, len(d.browseSelection) > 0)
	set(d.readSelectedBtn, idle && connected && len(d.browseSelection) > 0)
	set(d.pollBtn, idle && connected && !d.snapshot.Polling)
	set(d.stopPollBtn, d.snapshot.Polling)
	set(d.writeBtn, idle && connected && d.allowWrite.Checked)
	set(d.browseBtn, idle && connected)
	if idle {
		d.activity.Stop()
		d.activity.Hide()
	} else {
		d.activity.Show()
		d.activity.Start()
	}
}
func (d *Desktop) refresh(s service.Snapshot) {
	if d.revision == s.Revision {
		return
	}
	d.revision = s.Revision
	previous := d.snapshot
	d.snapshot = s
	if s.State == "connected" && (previous.State != "connected" || previous.Generation != s.Generation) {
		d.target.SetText(s.Target)
	}
	if previous.Generation != s.Generation {
		d.allowWrite.SetChecked(false)
	}
	state := map[string]string{"disconnected": "未连接", "connecting": "连接中…", "connected": "已连接", "error": "连接失败", "closed": "已关闭"}[s.State]
	d.status.SetText(state)
	d.status.Importance = widget.MediumImportance
	if s.State == "connected" {
		d.status.Importance = widget.SuccessImportance
	}
	if s.State == "error" {
		d.status.Importance = widget.DangerImportance
	}
	d.status.Refresh()
	if s.LastError != "" {
		d.showError(errors.New(s.LastError))
	}
	d.recordStatus.SetText("记录: " + s.RecordDir)
	if s.RecordError != "" {
		d.recordStatus.SetText("记录错误: " + s.RecordError)
		d.recordStatus.Importance = widget.DangerImportance
	} else {
		d.recordStatus.Importance = widget.MediumImportance
	}
	d.refreshPointRows()
	d.scanStatus.SetText(s.ScanStatus)
	// Nodes are copy-on-write: refresh the browse table only for a new list.
	var first *device.Node
	if len(s.Nodes) > 0 {
		first = &s.Nodes[0]
	}
	if first != d.nodesKey || len(s.Nodes) != d.nodesLen {
		d.nodesKey, d.nodesLen = first, len(s.Nodes)
		d.browseTable.Refresh()
	}
	if s.WriteResult != nil {
		d.writeStatus.SetText("写入: " + s.WriteResult.State + " · 读回" + s.Readback)
	} else {
		d.writeStatus.SetText("写入默认禁用；勾选允许写入后，每次操作仍须确认。")
	}
	d.logs.SetText(strings.Join(s.Logs, "\n"))
	var lines []string
	for _, e := range s.Packets {
		var targets []string
		for _, a := range e.Targets[:min(len(e.Targets), 3)] {
			targets = append(targets, a.NodeID)
		}
		target := strings.Join(targets, ", ")
		if n := max(len(e.Targets), e.Total); n > 3 {
			target += fmt.Sprintf(" 等 %d 个", n)
		}
		lines = append(lines, fmt.Sprintf("%s  #%d  %s  %s  %s  %s", e.Time.Format("15:04:05.000"), e.RequestID, e.Dir, e.Operation, e.Status, target))
	}
	d.events.SetText(strings.Join(lines, "\n"))
	d.controls()
}
func (d *Desktop) config() (service.Config, error) {
	cfg := service.DefaultConfig()
	cfg.Target = strings.TrimSpace(d.target.Text)
	cfg.Policy = d.policy.Selected
	cfg.Mode = d.mode.Selected
	cfg.Username = d.username.Text
	cfg.Password = d.password.Text
	cfg.Certificate = d.cert.Text
	cfg.PrivateKey = d.key.Text
	cfg.ServerCertificate = d.serverCert.Text
	var err error
	cfg.Timeout, err = time.ParseDuration(d.timeout.Text)
	if err != nil {
		return cfg, fmt.Errorf("超时须为 5s、500ms 等格式")
	}
	if err = opcua.ValidateEndpoint(cfg.Target); err != nil {
		return cfg, err
	}
	return cfg, cfg.Options.Validate()
}
func (d *Desktop) points() ([]service.Point, error) {
	n, err := strconv.Atoi(d.count.Text)
	if err != nil {
		return nil, fmt.Errorf("元素数量须为整数")
	}
	var pp []service.Point
	for _, line := range strings.Split(d.addresses.Text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		a, err := device.ParseAddress(line, device.DataType(d.kind.Selected), n)
		if err != nil {
			return nil, err
		}
		pp = append(pp, service.Point{Address: a.NodeID, Type: a.Type, Count: a.Count})
	}
	if len(pp) == 0 {
		return nil, fmt.Errorf("请输入至少一个 NodeID，每行一个")
	}
	return pp, nil
}
func (d *Desktop) saveInputs() {
	p := d.app.Preferences()
	p.SetString("endpoint", d.target.Text)
	p.SetString("policy", d.policy.Selected)
	p.SetString("mode", d.mode.Selected)
	p.SetString("timeout", d.timeout.Text)
	p.SetString("nodes", d.addresses.Text)
	p.SetString("type", d.kind.Selected)
	p.SetString("count", d.count.Text)
	p.SetString("cert", d.cert.Text)
	p.SetString("key", d.key.Text)
	p.SetString("server-cert", d.serverCert.Text)
}
func (d *Desktop) loadInputs() {
	p := d.app.Preferences()
	d.target.SetText(p.String("endpoint"))
	d.policy.SetSelected(p.StringWithFallback("policy", "None"))
	d.mode.SetSelected(p.StringWithFallback("mode", "None"))
	d.timeout.SetText(p.StringWithFallback("timeout", "5s"))
	d.addresses.SetText(p.StringWithFallback("nodes", simulator.CounterNode))
	d.kind.SetSelected(p.StringWithFallback("type", "Auto"))
	d.count.SetText(p.StringWithFallback("count", "1"))
	d.cert.SetText(p.String("cert"))
	d.key.SetText(p.String("key"))
	d.serverCert.SetText(p.String("server-cert"))
}
func (d *Desktop) copyEvents() {
	b, err := json.MarshalIndent(d.controller.Snapshot().Packets, "", "  ")
	if err != nil {
		d.showError(err)
		return
	}
	d.win.Clipboard().SetContent(string(b))
}
func (d *Desktop) confirmWrite() {
	if d.busy != 0 {
		return
	}
	p, err := d.singlePoint()
	if err != nil {
		d.showError(err)
		return
	}
	s := d.controller.Snapshot()
	if !d.allowWrite.Checked || s.State != "connected" {
		return
	}
	a, _ := device.ParseAddress(p.Address, p.Type, p.Count)
	if _, err = device.ParseValue(a, d.writeValue.Text); err != nil {
		d.showError(err)
		return
	}
	req := service.WriteRequest{Point: p, Value: d.writeValue.Text, Generation: s.Generation}
	dialog.ShowConfirm("确认 OPC UA 写入", fmt.Sprintf("Endpoint: %s\nNodeID: %s\n类型: %s，数量: %d\n值: %s", s.Target, req.Point.Address, req.Point.Type, req.Point.Count, req.Value), func(ok bool) {
		if ok && d.allowWrite.Checked {
			d.run(func(ctx context.Context) error { return d.controller.Write(ctx, req, true) }, nil)
		}
	}, d.win)
}
