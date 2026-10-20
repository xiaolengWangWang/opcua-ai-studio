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
	timeout, username, password, cert, key, serverCert, addresses, count, interval, writeValue, browseRoot           *widget.Entry
	policy, mode, kind                                                                                               *widget.Select
	allowWrite                                                                                                       *widget.Check
	status, lastError, recordStatus, writeStatus, logs                                                               *widget.Label
	activity                                                                                                         *widget.Activity
	connectBtn, disconnectBtn, demoBtn, stopDemoBtn, readBtn, pollBtn, stopPollBtn, writeBtn, browseBtn, discoverBtn *widget.Button
	table, browseTable                                                                                               *widget.Table
	resultFilter                                                                                                     *widget.Select
	allRoot                                                                                                          *widget.Entry
	verifyBtn, fullBtn, browseFullBtn, stopReadBtn, exportBtn                                                        *widget.Button
	scanStatus                                                                                                       *widget.Label
	pointSummary                                                                                                     *widget.RichText
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
	version                                                                                                          string
	updateBtn, checkUpdateBtn                                                                                        *widget.Button
	// Single point card.
	singleNode                 *widget.SelectEntry
	singleKind                 *widget.Select
	singleCount                *widget.Entry
	singleResult               *widget.Label
	singleKinds                map[string]string // the node list's points and their types
	singleIdent                string            // the shown point's viSCADA 属性标识
	copyIdentBtn, referenceBtn *widget.Button
	// Reference point, saved per endpoint and shown in the header.
	reference                *service.Point
	refBox                   *fyne.Container
	refLabel                 *widget.Label
	refCheckBtn, refClearBtn *widget.Button
	// Result search and tree.
	search               *widget.Entry
	searchMode, viewMode *widget.Select
	tree                 *widget.Tree
	treeIndex            pointTree
	// Connection profiles, server search and node search.
	profile                                                                                *widget.Select
	autoReconnect                                                                          *widget.Check
	scanBtn, serverInfoBtn, saveProfileBtn, deleteProfileBtn, nodeSearchBtn, stopSearchBtn *widget.Button
	securityBtn                                                                            *widget.Button
	// The address and port make the Endpoint; a full Endpoint's path is kept.
	host            *widget.SelectEntry // with the recent endpoints
	port            *widget.Entry
	endpointPath    string
	endpointSync    bool // the fields are being set from an Endpoint
	settings        dialog.Dialog
	securitySummary string
	securityLabel   *widget.Label
	autoFullRead    *widget.Check
	// Menu bar: items that mirror buttons, the toggles, and the last state shown.
	mainMenu                                                          *fyne.MainMenu
	menuButtons                                                       map[*fyne.MenuItem]*widget.Button
	menuAutoReconnect, menuAutoFullRead, menuAutoRefresh, menuSidebar *fyne.MenuItem
	menuViews                                                         []*fyne.MenuItem
	menuState                                                         string
	// Views besides the table and tree, and the values history behind trends.
	cards, trends             *widget.GridWrap
	trendRows                 []int
	history                   map[string][]float64
	historyFirst              *device.ReadResult
	foldBtn, writeSelectedBtn *widget.Button
	exportCSVBtn              *widget.Button
	summaryBase               []widget.RichTextSegment // the counts, before the read status
	selectedNode              string                   // the result last chosen in a view, for writing
	// Points tab layout.
	sidebar     *widget.Accordion
	emptyHint   *widget.Label
	writeChoice *widget.RadioGroup // true / false for a Boolean point
	treeSearch  string             // the search the tree was last opened for
	// Communication tab: the requests shown, newest first.
	packetTable               *widget.Table
	packetFilter              *widget.Select
	packetSearch              *widget.Entry
	packetStats, packetDetail *widget.Label
	packets                   []device.Event
	packetsKey                string
	packetsSince              time.Time // 清空显示 hides older requests
	// Result table: the sort column, -1 for none, and values seen changing.
	sortCol    int
	sortDesc   bool
	lastValues map[string]string
	changedAt  map[string]time.Time
	// Auto refresh of the shown points, and the folding side column.
	autoRefresh     *widget.Check
	refreshInterval *widget.Select
	syncing         bool // the check follows the snapshot, not a click
	split           *container.Split
	splitOffset     float64 // the side column width to restore after folding
	nodeSearch      *widget.Entry
	nodeSearchMode  *widget.RadioGroup
	browseStatus    *widget.Label
}

func NewDesktop(a fyne.App, c *service.Controller, version string) *Desktop {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Desktop{app: a, controller: c, ctx: ctx, cancel: cancel, version: version, sortCol: -1}
	d.win = a.NewWindow("OPC UA AI Studio " + version)
	d.win.SetIcon(icon.Application())
	d.status = widget.NewLabelWithStyle("未连接", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	d.lastError = widget.NewLabel("")
	d.lastError.Importance = widget.DangerImportance
	d.lastError.Wrapping = fyne.TextWrapWord
	d.lastError.Hide()
	d.activity = widget.NewActivity()
	d.activity.Hide()
	d.recordStatus = widget.NewLabel("")
	d.recordStatus.Truncation = fyne.TextTruncateEllipsis
	d.recordStatus.Importance = widget.LowImportance
	d.refLabel = widget.NewLabel("")
	d.refCheckBtn = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { d.run(d.controller.CheckReference, nil) })
	d.refClearBtn = widget.NewButtonWithIcon("", theme.ContentClearIcon(), d.clearReference)
	d.refBox = container.NewHBox(d.refLabel, d.refCheckBtn, d.refClearBtn)
	d.refBox.Hide()
	d.checkUpdateBtn = widget.NewButtonWithIcon("检查更新", theme.ViewRefreshIcon(), func() { d.checkUpdates(false) })
	d.updateBtn = widget.NewButtonWithIcon("", theme.DownloadIcon(), nil)
	d.updateBtn.Importance = widget.HighImportance
	d.updateBtn.Hide()
	connection := d.connectionPanel()
	d.tabs = container.NewAppTabs(container.NewTabItemWithIcon("点位与读写", theme.ListIcon(), d.pointsPanel()), container.NewTabItemWithIcon("节点浏览", theme.SearchIcon(), d.browsePanel()), container.NewTabItemWithIcon("通讯与日志", theme.DocumentIcon(), d.diagnosticsPanel()), container.NewTabItemWithIcon("AI 诊断", theme.InfoIcon(), d.aiPanel()))
	// The window title names the program; the rare actions are in the menu
	// bar, so only the connection bar sits above the data.
	d.buildMenu()
	d.win.SetContent(container.NewPadded(container.NewBorder(container.NewVBox(connection, d.lastError), nil, nil, nil, d.tabs)))
	// The last window size, else 1280 × 860; either within the work area, so
	// the window fits a 1366 × 768 laptop.
	w, h := platform.WorkArea()
	want := fyne.NewSize(1280, 860)
	if p := a.Preferences(); p.Float("window-width") >= 800 && p.Float("window-height") >= 560 {
		want = fyne.NewSize(float32(p.Float("window-width")), float32(p.Float("window-height")))
	}
	size := platform.FitSize(want, w, h, d.win.Canvas().Scale())
	d.win.Resize(size)
	// A narrow screen starts with a narrower side column.
	if size.Width < 1200 {
		d.splitOffset = .24
		d.split.SetOffset(d.splitOffset)
	}
	d.loadInputs()
	d.refresh(c.Snapshot())
	d.win.SetCloseIntercept(d.quit)
	return d
}

// quit records and releases everything before the app exits.
func (d *Desktop) quit() {
	size := d.win.Canvas().Size()
	d.app.Preferences().SetFloat("window-width", float64(size.Width))
	d.app.Preferences().SetFloat("window-height", float64(size.Height))
	d.cancel()
	go func() { d.Shutdown(); fyne.Do(func() { d.win.SetCloseIntercept(nil); d.win.Close(); d.app.Quit() }) }()
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
	set(d.deleteProfileBtn, d.profile.Selected != "")
	set(d.serverInfoBtn, idle && connected)
	set(d.refCheckBtn, idle && connected)
	set(d.referenceBtn, idle && connected)
	set(d.copyIdentBtn, d.singleIdent != "")
	set(d.nodeSearchBtn, idle && connected)
	set(d.stopSearchBtn, d.checkCancel != nil)
	// Disconnecting also stops an automatic reconnect.
	set(d.disconnectBtn, idle && (connected || d.snapshot.State == "reconnecting"))
	set(d.demoBtn, idle && d.snapshot.DemoTarget == "")
	set(d.stopDemoBtn, idle && d.snapshot.DemoTarget != "")
	// One button per state: connect or disconnect, start or stop the demo.
	show := func(o fyne.CanvasObject, on bool) {
		if on != o.Visible() {
			if on {
				o.Show()
			} else {
				o.Hide()
			}
		}
	}
	session := connected || d.snapshot.State == "reconnecting" || d.snapshot.State == "connecting"
	show(d.connectBtn, !session)
	show(d.disconnectBtn, session)
	show(d.demoBtn, d.snapshot.DemoTarget == "")
	show(d.stopDemoBtn, d.snapshot.DemoTarget != "")
	set(d.readBtn, idle && connected)
	set(d.verifyBtn, idle && connected)
	set(d.fullBtn, idle && connected)
	set(d.browseFullBtn, idle && connected)
	set(d.stopReadBtn, d.checkCancel != nil)
	set(d.exportBtn, idle && len(d.snapshot.Results) > 0)
	set(d.exportCSVBtn, idle && len(d.snapshot.Results) > 0)
	r, chosen := d.selectedResult()
	set(d.writeSelectedBtn, idle && connected && chosen && (r.Writable() || r.Access == ""))
	set(d.exportAttrBtn, idle && len(d.snapshot.Results) > 0)
	set(d.importBtn, idle && connected)
	set(d.selectAllBtn, len(d.snapshot.Nodes) > 0)
	set(d.clearSelectBtn, len(d.browseSelection) > 0)
	set(d.readSelectedBtn, idle && connected && len(d.browseSelection) > 0)
	set(d.pollBtn, idle && connected && !d.snapshot.Polling)
	set(d.stopPollBtn, d.snapshot.Polling)
	set(d.writeBtn, idle && connected && d.allowWrite.Checked)
	set(d.browseBtn, idle && connected)
	// The check follows the controller: a lost connection or a list poll stops it.
	if d.autoRefresh.Checked != d.snapshot.AutoRefresh {
		d.syncing = true
		d.autoRefresh.SetChecked(d.snapshot.AutoRefresh)
		d.syncing = false
	}
	if connected && len(d.snapshot.Results) > 0 {
		d.autoRefresh.Enable()
	} else if !d.snapshot.AutoRefresh {
		d.autoRefresh.Disable()
	}
	if idle {
		d.activity.Stop()
		d.activity.Hide()
	} else {
		d.activity.Show()
		d.activity.Start()
	}
	d.updateMenu()
}
func (d *Desktop) refresh(s service.Snapshot) {
	if d.revision == s.Revision {
		return
	}
	d.revision = s.Revision
	previous := d.snapshot
	d.snapshot = s
	if s.State == "connected" && (previous.State != "connected" || previous.Generation != s.Generation) {
		d.setEndpoint(s.Target)
	}
	if previous.Generation != s.Generation {
		d.allowWrite.SetChecked(false)
	}
	state := map[string]string{"disconnected": "未连接", "connecting": "连接中…", "connected": "已连接", "reconnecting": "自动重连中…", "error": "连接失败", "closed": "已关闭"}[s.State]
	if s.State == "connected" && s.Server != nil && s.Server.Product != "" {
		state += " · " + strings.TrimSpace(s.Server.Product+" "+s.Server.Version)
	}
	d.status.SetText(state)
	// With several windows open, the title tells them apart on the taskbar.
	title := "OPC UA AI Studio " + d.version
	if s.State == "connected" || s.State == "reconnecting" {
		title += " · " + strings.TrimPrefix(s.Target, "opc.tcp://")
	}
	if title != d.win.Title() {
		d.win.SetTitle(title)
	}
	d.status.Importance = widget.MediumImportance
	switch s.State {
	case "connected":
		d.status.Importance = widget.SuccessImportance
	case "reconnecting":
		d.status.Importance = widget.WarningImportance
	case "error":
		d.status.Importance = widget.DangerImportance
	}
	d.status.Refresh()
	if s.LastError != "" {
		d.showError(errors.New(s.LastError))
	} else if previous.LastError != "" {
		// An automatic reconnect or a later operation cleared the error.
		d.lastError.Hide()
	}
	// The record path is in 帮助 › 关于; a recording problem shows as an error.
	if s.RecordError != "" && s.RecordError != previous.RecordError {
		d.showError(errors.New("记录错误：" + s.RecordError))
	}
	d.refreshPointRows()
	d.refreshReference()
	d.updateSummary()
	d.browseStatus.SetText(s.BrowseStatus)
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
	d.refreshPackets(false)
	d.controls()
}
func (d *Desktop) config() (service.Config, error) {
	cfg := service.DefaultConfig()
	cfg.Target = normalizeEndpoint(d.endpoint())
	if cfg.Target != "" {
		d.setEndpoint(cfg.Target)
	}
	cfg.AutoReconnect = d.autoReconnect.Checked
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
		a, _, err := parsePoint(line, device.DataType(d.kind.Selected), n)
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
	p.SetString("endpoint", d.endpoint())
	p.SetString("policy", d.policy.Selected)
	p.SetString("mode", d.mode.Selected)
	p.SetString("timeout", d.timeout.Text)
	p.SetString("nodes", d.addresses.Text)
	p.SetString("type", d.kind.Selected)
	p.SetString("count", d.count.Text)
	p.SetString("cert", d.cert.Text)
	p.SetString("key", d.key.Text)
	p.SetString("server-cert", d.serverCert.Text)
	p.SetBool("auto-reconnect", d.autoReconnect.Checked)
	p.SetBool("auto-full-read", d.autoFullRead.Checked)
}
func (d *Desktop) loadInputs() {
	p := d.app.Preferences()
	d.setEndpoint(p.String("endpoint"))
	d.host.SetOptions(historyOptions(d.endpointHistory()))
	d.policy.SetSelected(p.StringWithFallback("policy", "None"))
	d.mode.SetSelected(p.StringWithFallback("mode", "None"))
	d.timeout.SetText(p.StringWithFallback("timeout", "5s"))
	d.addresses.SetText(p.StringWithFallback("nodes", simulator.CounterNode))
	d.kind.SetSelected(p.StringWithFallback("type", "Auto"))
	d.count.SetText(p.StringWithFallback("count", "1"))
	d.cert.SetText(p.String("cert"))
	d.key.SetText(p.String("key"))
	d.serverCert.SetText(p.String("server-cert"))
	d.autoReconnect.SetChecked(p.BoolWithFallback("auto-reconnect", true))
	d.autoFullRead.SetChecked(p.Bool("auto-full-read"))
	d.storeProfiles(d.profiles(), "")
}
func (d *Desktop) confirmWrite() {
	if d.busy != 0 {
		return
	}
	p, _, err := d.singlePoint()
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
	// The current value shows what the write changes.
	current := "未读取"
	for i := range s.Results {
		if r := &s.Results[i]; r.Address.NodeID == p.Address && r.Value.Type != "" {
			current = device.FormatValue(r.Value)
		}
	}
	dialog.ShowConfirm("确认 OPC UA 写入", fmt.Sprintf("Endpoint: %s\nNodeID: %s\n类型: %s，数量: %d\n当前值: %s\n写入值: %s", s.Target, req.Point.Address, req.Point.Type, req.Point.Count, current, req.Value), func(ok bool) {
		if ok && d.allowWrite.Checked {
			d.run(func(ctx context.Context) error { return d.controller.Write(ctx, req, true) }, nil)
		}
	}, d.win)
}
