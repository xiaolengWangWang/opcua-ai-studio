package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
	"net"
	"net/url"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/simulator"
	"strings"
	"time"
)

// connectionPanel is the single top bar: a saved connection, the address and
// port, the connection settings, the server search and connect or disconnect,
// then the status. The rarer connection actions live in the 连接 menu; their
// buttons are kept for the menu and are not placed.
func (d *Desktop) connectionPanel() fyne.CanvasObject {
	d.host = widget.NewSelectEntry(nil)
	d.host.SetPlaceHolder("IP 地址或主机名，如 192.168.1.10；下拉为最近连接")
	d.host.OnChanged = d.hostChanged
	d.port = widget.NewEntry()
	d.port.SetPlaceHolder("4840")
	d.port.SetText("4840")
	connect := func() {
		cfg, err := d.config()
		if err != nil {
			d.showError(err)
			return
		}
		d.loadReference(d.referenceKey(cfg.Target))
		d.run(func(ctx context.Context) error { return d.controller.Connect(ctx, cfg) }, func() {
			d.saveInputs()
			d.rememberEndpoint(cfg.Target)
			if d.autoFullRead.Checked {
				d.startFullRead(d.allRoot.Text)
			}
		})
	}
	d.host.OnSubmitted = func(string) { connect() }
	d.port.OnSubmitted = func(string) { connect() }
	d.connectBtn = widget.NewButtonWithIcon("连接", theme.LoginIcon(), connect)
	d.connectBtn.Importance = widget.HighImportance
	d.disconnectBtn = widget.NewButtonWithIcon("断开", theme.LogoutIcon(), func() {
		d.run(func(context.Context) error { return d.controller.Disconnect() }, nil)
	})
	d.demoBtn = widget.NewButtonWithIcon("本机演示", theme.MediaPlayIcon(), func() {
		d.loadReference(demoReference)
		d.run(d.controller.StartDemo, func() {
			d.setEndpoint(d.controller.Snapshot().DemoTarget)
			d.profile.ClearSelected()
			d.policy.SetSelected("None")
			d.mode.SetSelected("None")
			d.addresses.SetText(strings.Join(append([]string{simulator.CounterNode}, simulator.ChineseNodes...), "\n"))
			d.singleNode.SetText(simulator.CounterNode)
			d.allRoot.SetText("ns=2")
			d.browseRoot.SetText("ns=2")
			d.kind.SetSelected("Auto")
			d.count.SetText("1")
			d.singleKind.SetSelected("Int32")
			d.singleCount.SetText("1")
		})
	})
	d.stopDemoBtn = widget.NewButtonWithIcon("停止演示", theme.MediaStopIcon(), func() {
		d.run(func(context.Context) error { return d.controller.StopDemo() }, nil)
	})
	d.discoverBtn = widget.NewButtonWithIcon("发现端点", theme.ListIcon(), func() {
		cfg, err := d.config()
		if err != nil {
			d.showError(err)
			return
		}
		var ee []opcua.Endpoint
		d.run(func(ctx context.Context) error {
			var e error
			ee, e = opcua.Endpoints(ctx, cfg.Target, cfg.Timeout)
			return e
		}, func() { d.endpointsDialog(ee) })
	})
	d.scanBtn = widget.NewButtonWithIcon("搜索服务器", theme.SearchIcon(), d.serverSearchDialog)
	d.timeout = widget.NewEntry()
	d.timeout.SetText("5s")
	d.username = widget.NewEntry()
	d.password = widget.NewPasswordEntry()
	d.cert = widget.NewEntry()
	d.key = widget.NewEntry()
	d.serverCert = widget.NewEntry()
	d.policy = widget.NewSelect([]string{"None", "Basic256Sha256", "Aes128_Sha256_RsaOaep", "Aes256_Sha256_RsaPss"}, nil)
	d.policy.SetSelected("None")
	d.mode = widget.NewSelect([]string{"None", "Sign", "SignAndEncrypt"}, nil)
	d.mode.SetSelected("None")
	security := container.NewGridWithColumns(2, widget.NewForm(widget.NewFormItem("安全策略", d.policy), widget.NewFormItem("安全模式", d.mode), widget.NewFormItem("超时", d.timeout), widget.NewFormItem("用户名", d.username), widget.NewFormItem("密码", d.password)), widget.NewForm(widget.NewFormItem("客户端证书", d.cert), widget.NewFormItem("客户端私钥", d.key), widget.NewFormItem("可信服务器证书", d.serverCert)))
	d.autoReconnect = widget.NewCheck("断线自动重连", func(bool) { d.updateMenu() })
	d.autoReconnect.SetChecked(true)
	d.autoFullRead = widget.NewCheck("连接后自动全量读取", func(bool) { d.updateMenu() })
	d.settings = d.settingsDialog(security)
	d.securityBtn = widget.NewButtonWithIcon("", theme.SettingsIcon(), func() { d.showSecurity(true) })
	summary := func(string) { d.updateSecuritySummary() }
	d.policy.OnChanged, d.mode.OnChanged, d.username.OnChanged = summary, summary, summary
	d.probeBtn = widget.NewButtonWithIcon("连接数测试", theme.ComputerIcon(), d.probeSessionsDialog)
	d.serverInfoBtn = widget.NewButtonWithIcon("服务器信息", theme.InfoIcon(), d.serverInfoDialog)
	d.profile = widget.NewSelect(nil, d.applyProfile)
	d.profile.PlaceHolder = "已保存的连接"
	d.saveProfileBtn = widget.NewButtonWithIcon("保存配置", theme.DocumentSaveIcon(), d.saveProfileDialog)
	d.deleteProfileBtn = widget.NewButtonWithIcon("删除配置", theme.DeleteIcon(), d.deleteProfile)
	d.updateSecuritySummary()
	small := func(text string) *widget.Label {
		return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	d.historyBtn = widget.NewButtonWithIcon("", theme.HistoryIcon(), d.connectionHistoryDialog)
	h := d.profile.MinSize().Height
	profile := container.NewGridWrap(fyne.NewSize(170, h), d.profile)
	port := container.NewGridWrap(fyne.NewSize(76, h), d.port)
	d.status.Truncation = fyne.TextTruncateEllipsis
	status := container.NewGridWrap(fyne.NewSize(230, h), d.status)
	// The busy indicator and the reference point keep their width, so the
	// address does not narrow while an operation such as a write runs.
	slot := canvas.NewRectangle(color.Transparent)
	slot.SetMinSize(fyne.NewSize(h, h))
	busy := container.NewStack(slot, d.activity)
	return container.NewBorder(nil, nil, container.NewHBox(profile, d.historyBtn, small("地址")),
		container.NewHBox(small("端口"), port, d.securityBtn, d.scanBtn, d.connectBtn, d.disconnectBtn, d.refBox, busy, status, d.updateBtn), d.host)
}

// updateSecuritySummary names the security in force at the top of the
// settings dialog.
func (d *Desktop) updateSecuritySummary() {
	if d.securityBtn == nil || d.policy == nil || d.mode == nil || d.username == nil {
		return
	}
	auth := "匿名"
	if u := strings.TrimSpace(d.username.Text); u != "" {
		auth = "用户 " + u
	}
	d.securitySummary = fmt.Sprintf("%s / %s · %s", d.policy.Selected, d.mode.Selected, auth)
	d.securityLabel.SetText("当前：" + d.securitySummary)
}

// showSecurity opens the connection settings; false is kept for callers that
// only open them when needed.
func (d *Desktop) showSecurity(open bool) {
	if open {
		d.settings.Show()
	}
}

// endpoint composes the Endpoint from the address and port fields; an address
// typed as a full Endpoint keeps its own port and path.
func (d *Desktop) endpoint() string {
	host := strings.TrimSpace(d.host.Text)
	if host == "" || strings.Contains(host, "://") {
		return host
	}
	port := strings.TrimSpace(d.port.Text)
	if port == "" {
		port = "4840"
	}
	return "opc.tcp://" + net.JoinHostPort(host, port) + d.endpointPath
}

// setEndpoint splits an Endpoint, host:port or host into the fields.
func (d *Desktop) setEndpoint(s string) {
	d.endpointSync = true
	defer func() { d.endpointSync = false }()
	s = strings.TrimSpace(s)
	if s == "" {
		d.host.SetText("")
		d.endpointPath = ""
		return
	}
	full := normalizeEndpoint(s)
	u, err := url.Parse(full)
	if err != nil || u.Hostname() == "" {
		d.host.SetText(s)
		return
	}
	d.host.SetText(u.Hostname())
	d.port.SetText(u.Port())
	d.endpointPath = strings.TrimSuffix(u.EscapedPath(), "/")
}

// hostChanged splits a pasted Endpoint or a chosen recent one; typing a new
// address drops the path of the previous one.
func (d *Desktop) hostChanged(text string) {
	if d.endpointSync {
		return
	}
	t := strings.TrimSpace(text)
	if strings.Contains(t, "://") || strings.Count(t, ":") == 1 {
		d.setEndpoint(t)
		return
	}
	d.endpointPath = ""
}

func (d *Desktop) pointsPanel() fyne.CanvasObject {
	d.addresses = widget.NewMultiLineEntry()
	d.addresses.SetMinRowsVisible(4)
	d.addresses.SetPlaceHolder("每行一个 NodeID 或 viSCADA 属性标识，例如\nns=2;s=Counter、2::s::Counter::DINT\n浏览页点击变量会追加到这里")
	d.kind = widget.NewSelect(device.Types, nil)
	d.kind.SetSelected("Auto")
	d.count = widget.NewEntry()
	d.count.SetText("1")
	d.interval = widget.NewEntry()
	d.interval.SetText("1s")
	read := func() {
		pp, err := d.points()
		if err != nil {
			d.showError(err)
			return
		}
		d.runCheck(func(ctx context.Context) error { return d.controller.Read(ctx, pp) }, d.saveInputs)
	}
	d.readBtn = widget.NewButtonWithIcon("读取清单", theme.ViewRefreshIcon(), read)
	d.pollBtn = widget.NewButtonWithIcon("开始轮询", theme.MediaPlayIcon(), func() {
		pp, err := d.points()
		if err != nil {
			d.showError(err)
			return
		}
		interval, err := time.ParseDuration(d.interval.Text)
		if err != nil {
			d.showError(err)
			return
		}
		d.run(func(ctx context.Context) error { return d.controller.StartPolling(ctx, pp, interval) }, nil)
	})
	d.stopPollBtn = widget.NewButtonWithIcon("停止轮询", theme.MediaPauseIcon(), func() { d.controller.StopPolling(); d.refresh(d.controller.Snapshot()) })
	// The single point card writes through the write dialog.
	d.singleWriteBtn = widget.NewButtonWithIcon("写入…", theme.DocumentCreateIcon(), d.writeSingle)
	d.singleNode = widget.NewSelectEntry(nil)
	d.singleNode.SetPlaceHolder("NodeID 或 viSCADA 属性标识，可下拉选择")
	d.singleNode.OnChanged = func(text string) {
		if kind, ok := d.singleKinds[text]; ok {
			d.singleKind.SetSelected(kind)
			d.singleCount.SetText("1")
		}
	}
	d.singleKind = widget.NewSelect(device.Types, nil)
	d.singleKind.SetSelected("Auto")
	d.singleCount = widget.NewEntry()
	d.singleCount.SetText("1")
	d.verifyBtn = widget.NewButtonWithIcon("单点验证", theme.SearchIcon(), d.verifySingle)
	d.verifyBtn.Importance = widget.HighImportance
	d.singleNode.OnSubmitted = func(string) { d.verifyBtn.OnTapped() }
	d.singleResult = widget.NewLabel("")
	d.singleResult.Wrapping = fyne.TextWrapWord
	d.singleResult.Selectable = true
	d.singleResult.Hide()
	d.copyIdentBtn = widget.NewButtonWithIcon("复制属性标识", theme.ContentCopyIcon(), func() {
		if d.singleIdent != "" {
			d.win.Clipboard().SetContent(d.singleIdent)
		}
	})
	d.referenceBtn = widget.NewButtonWithIcon("设为基准点位", theme.ConfirmIcon(), d.setReference)
	d.allRoot = widget.NewSelectEntry([]string{"i=85"})
	d.allRoot.SetText("i=85")
	d.allRoot.SetPlaceHolder("i=85 读取全部对象；ns=2 读取命名空间 2")
	d.fullBtn = widget.NewButtonWithIcon("全量读取", theme.DownloadIcon(), func() { d.startFullRead(d.allRoot.Text) })
	d.fullBtn.Importance = widget.HighImportance
	d.stopReadBtn = widget.NewButtonWithIcon("停止读取", theme.MediaStopIcon(), func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	})
	d.pointSummary = widget.NewRichText()
	d.pointSummary.Wrapping = fyne.TextWrapWord
	d.scanStatus = widget.NewLabel("")
	d.scanStatus.Wrapping = fyne.TextWrapWord
	d.emptyHint = widget.NewLabel("暂无结果。连接后在左侧单点验证、读取清单或全量读取，也可在“节点浏览”中勾选节点读取；没有服务器时点击“本机演示”体验。")
	d.emptyHint.Wrapping = fyne.TextWrapWord
	d.emptyHint.Importance = widget.LowImportance
	refreshRows := func(string) {
		if d.table != nil {
			d.refreshPointRows()
		}
	}
	d.resultFilter = widget.NewSelect(filters, refreshRows)
	d.resultFilter.SetSelected("全部")
	d.search = widget.NewEntry()
	d.search.SetPlaceHolder("搜索 NodeID、名称或路径，空格分隔多个关键字")
	d.search.OnChanged = refreshRows
	d.searchMode = widget.NewSelect(searchModes, refreshRows)
	d.searchMode.SetSelected("模糊")
	clearSearch := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() { d.search.SetText("") })
	d.exportCSVBtn = widget.NewButtonWithIcon("导出 CSV", theme.DocumentSaveIcon(), d.exportPointsDialog)
	d.exportAttrBtn = widget.NewButtonWithIcon("导出属性表", theme.DocumentSaveIcon(), d.exportAttributesDialog)
	// One button holds the exports, which share the results' filter and search.
	d.exportBtn = widget.NewButtonWithIcon("导出", theme.DocumentSaveIcon(), nil)
	d.exportBtn.OnTapped = func() {
		menu := fyne.NewMenu("",
			fyne.NewMenuItem("导出 CSV…", d.exportPointsDialog),
			fyne.NewMenuItem("导出 viSCADA 属性表…", d.exportAttributesDialog),
			fyne.NewMenuItem("复制结果", d.copyPointRows))
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(d.exportBtn).Add(fyne.NewPos(0, d.exportBtn.Size().Height))
		widget.ShowPopUpMenuAtPosition(menu, d.win.Canvas(), pos)
	}
	d.writeSelectedBtn = widget.NewButtonWithIcon("写入", theme.DocumentCreateIcon(), func() {
		if r, ok := d.selectedResult(); ok {
			d.writeDialog(r)
		}
	})
	d.importBtn = widget.NewButtonWithIcon("导入属性表验证", theme.FolderOpenIcon(), d.importAttributesDialog)
	d.jobLabel = widget.NewLabel("")
	d.jobBar = widget.NewProgressBar()
	d.jobBox = container.NewBorder(nil, nil, d.jobLabel, widget.NewButton("取消", func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	}), d.jobBar)
	d.jobBox.Hide()
	d.table = newTextTable(func() int { return len(d.visible) }, pointHeader, d.pointCell, d.pointStyle, pointWidths)
	sortableHeader(d.table, pointHeader, d.sortMark, d.sortBy)
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(d.visible) {
			return
		}
		r := d.snapshot.Results[d.visible[id.Row]]
		d.selectResult(r)
		// Clicking a control point's value opens the write dialog; the cell
		// is unselected so the next click opens it again.
		if id.Col == 4 && r.Writable() && d.snapshot.State == "connected" && d.busy == 0 {
			d.table.UnselectAll()
			d.writeDialog(r)
		}
	}
	d.tree = d.newPointTree()
	d.cards = d.newCardView()
	d.trends = d.newTrendView()
	expand := widget.NewButtonWithIcon("", theme.MenuExpandIcon(), func() { d.tree.OpenAllBranches() })
	collapse := widget.NewButtonWithIcon("", theme.MenuDropUpIcon(), func() { d.tree.CloseAllBranches() })
	treeTools := container.NewHBox(expand, collapse)
	d.trendRange = widget.NewSelect(trendRanges, nil)
	d.trendRange.SetSelected(trendRanges[0])
	d.trendRange.OnChanged = func(string) {
		d.resetTrends()
		d.refreshViews()
	}
	trendTools := container.NewGridWrap(fyne.NewSize(92, d.trendRange.MinSize().Height), d.trendRange)
	views := map[string]fyne.CanvasObject{"表格": d.table, "树状": d.tree, "卡片": d.cards, "趋势": d.trends}
	d.viewMode = widget.NewSelect(viewModes, func(mode string) {
		if d.tree == nil {
			return
		}
		for name, v := range views {
			if name == mode {
				v.Show()
			} else {
				v.Hide()
			}
		}
		if mode == "树状" {
			treeTools.Show()
			d.treeIndex = pointTree{}
			d.refreshTree()
		} else {
			treeTools.Hide()
		}
		if mode == "趋势" {
			trendTools.Show()
		} else {
			trendTools.Hide()
		}
		d.refreshViews()
		d.updateMenu()
	})
	d.viewMode.SetSelected("表格")
	d.refreshInterval = widget.NewSelect([]string{"1s", "2s", "5s", "10s", "30s"}, func(string) {
		if d.autoRefresh != nil && d.autoRefresh.Checked {
			d.startRefresh()
		}
	})
	d.refreshInterval.SetSelected("2s")
	d.autoRefresh = widget.NewCheck("自动刷新", func(on bool) {
		switch {
		case d.syncing:
		case on:
			d.startRefresh()
		default:
			d.controller.StopPolling()
			d.refresh(d.controller.Snapshot())
		}
	})
	d.foldBtn = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), d.toggleSidebar)
	form := func(items ...*widget.FormItem) *widget.Form { return widget.NewForm(items...) }
	single := container.NewVBox(d.singleNode,
		form(widget.NewFormItem("类型", d.singleKind), widget.NewFormItem("数量", d.singleCount)),
		container.NewGridWithColumns(2, d.verifyBtn, d.singleWriteBtn), d.singleResult, container.NewGridWithColumns(2, d.copyIdentBtn, d.referenceBtn))
	list := container.NewVBox(d.addresses,
		form(widget.NewFormItem("类型", d.kind), widget.NewFormItem("数量", d.count), widget.NewFormItem("轮询间隔", d.interval)),
		container.NewGridWithColumns(3, d.readBtn, d.pollBtn, d.stopPollBtn))
	full := container.NewVBox(form(widget.NewFormItem("父节点", d.allRoot)), container.NewGridWithColumns(2, d.fullBtn, d.stopReadBtn))
	attributes := container.NewVBox(d.importBtn, widget.NewLabelWithStyle("导出在结果栏“导出”中，按当前筛选和搜索导出。", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	// Sections fold, so the column fits a laptop screen; only the single
	// point section starts open.
	d.sidebar = widget.NewAccordion(
		widget.NewAccordionItem("单点验证与写入", single),
		widget.NewAccordionItem("节点清单与轮询", list),
		widget.NewAccordionItem("全量读取", full),
		widget.NewAccordionItem("viSCADA 属性表", attributes),
	)
	d.sidebar.MultiOpen = true
	d.sidebar.Open(0)
	// One toolbar row: the search fills what the controls leave, and the
	// summary line also carries the read status.
	fixed := func(w float32, o fyne.CanvasObject) fyne.CanvasObject {
		return container.NewGridWrap(fyne.NewSize(w, o.MinSize().Height), o)
	}
	controls := container.NewHBox(clearSearch, fixed(76, d.searchMode), fixed(124, d.resultFilter), fixed(80, d.viewMode), treeTools, trendTools, d.autoRefresh, fixed(64, d.refreshInterval), d.writeSelectedBtn, d.exportBtn)
	toolbar := container.NewBorder(nil, nil, d.foldBtn, controls, d.search)
	// The ? before the counts explains GOOD, BAD and the others.
	qualityHelp := widget.NewButtonWithIcon("", theme.QuestionIcon(), d.showQualityHelp)
	qualityHelp.Importance = widget.LowImportance
	summary := container.NewBorder(nil, nil, qualityHelp, nil, d.pointSummary)
	results := container.NewBorder(container.NewVBox(toolbar, summary, d.jobBox, d.emptyHint), nil, nil, nil, container.NewStack(d.table, d.tree, d.cards, d.trends))
	// In a box the sections keep their own height instead of sharing the
	// column's, which left gaps under the open ones.
	d.split = container.NewHSplit(container.NewVScroll(container.NewVBox(d.sidebar)), results)
	d.splitOffset = .26
	d.split.SetOffset(d.splitOffset)
	return d.split
}

func (d *Desktop) browsePanel() fyne.CanvasObject {
	d.browseRoot = widget.NewSelectEntry([]string{"i=85"})
	d.browseRoot.SetText("i=85")
	d.browseRoot.SetPlaceHolder("i=85 为 Objects；ns=2 为命名空间 2 的节点")
	browse := func() {
		node := d.browseRoot.Text
		d.rememberRoot(node)
		d.run(func(ctx context.Context) error { return d.controller.Browse(ctx, node) }, nil)
	}
	d.browseRoot.OnSubmitted = func(string) { browse() }
	d.browseBtn = widget.NewButton("浏览子节点", browse)
	d.browseFullBtn = widget.NewButton("读取此节点下全部变量", func() { d.startFullRead(d.browseRoot.Text) })
	d.nodeSearch = widget.NewEntry()
	d.nodeSearch.SetPlaceHolder("在父节点下按名称、NodeID 搜索节点，例如 温度 或 Station001 Tag")
	d.nodeSearchMode = widget.NewRadioGroup(searchModes, nil)
	d.nodeSearchMode.Horizontal = true
	d.nodeSearchMode.Required = true
	d.nodeSearchMode.SetSelected("模糊")
	d.nodeSearchBtn = widget.NewButtonWithIcon("搜索节点", theme.SearchIcon(), d.searchNodes)
	d.nodeSearch.OnSubmitted = func(string) { d.nodeSearchBtn.OnTapped() }
	d.stopSearchBtn = widget.NewButtonWithIcon("停止", theme.MediaStopIcon(), func() {
		if d.checkCancel != nil {
			d.checkCancel()
		}
	})
	d.browseStatus = widget.NewLabel("")
	d.browseStatus.Importance = widget.LowImportance
	d.browseSelected = map[string]bool{}
	d.selectionLabel = widget.NewLabel("")
	d.selectAllBtn = widget.NewButton("全选", func() {
		for _, n := range d.snapshot.Nodes {
			d.selectNode(n, true)
		}
		d.selectionChanged()
	})
	d.clearSelectBtn = widget.NewButton("清除选择", func() {
		d.browseSelection, d.browseSelected = nil, map[string]bool{}
		d.selectionChanged()
	})
	d.readSelectedBtn = widget.NewButtonWithIcon("读取选中节点", theme.ViewRefreshIcon(), func() {
		roots := append([]device.Node(nil), d.browseSelection...)
		d.tabs.SelectIndex(0)
		d.runCheck(func(ctx context.Context) error { return d.controller.ReadNodes(ctx, roots) }, nil)
	})
	d.browseTable = newTextTable(func() int { return len(d.snapshot.Nodes) }, browseHeader, d.browseCell, d.browseStyle, []float32{50, 420, 320, 210})
	d.browseTable.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(d.snapshot.Nodes) {
			return
		}
		n := d.snapshot.Nodes[id.Row]
		if id.Col == 0 {
			d.selectNode(n, !d.browseSelected[n.NodeID])
			d.selectionChanged()
			d.browseTable.UnselectAll()
			return
		}
		if strings.Contains(n.Class, "Variable") {
			d.appendPoint(n.NodeID)
			d.singleNode.SetText(n.NodeID)
			d.singleKind.SetSelected("Auto")
			d.singleCount.SetText("1")
			d.tabs.SelectIndex(0)
		} else {
			d.browseRoot.SetText(n.NodeID)
		}
	}
	d.browseBtn.SetIcon(theme.NavigateNextIcon())
	d.readSelectedBtn.Importance = widget.HighImportance
	// The selection and the browse status share one row; how to use the list
	// is in the manual.
	selection := container.NewBorder(nil, nil, container.NewHBox(d.selectAllBtn, d.clearSelectBtn, d.readSelectedBtn, d.selectionLabel), nil, d.browseStatus)
	search := container.NewBorder(nil, nil, widget.NewLabelWithStyle("搜索", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(d.nodeSearchMode, d.nodeSearchBtn, d.stopSearchBtn), d.nodeSearch)
	top := container.NewVBox(container.NewBorder(nil, nil, widget.NewLabelWithStyle("父节点", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(d.browseBtn, d.browseFullBtn), d.browseRoot), search, selection)
	return container.NewBorder(top, nil, nil, nil, d.browseTable)
}

// searchNodes searches below the browse parent; the matches replace the
// browse list.
func (d *Desktop) searchNodes() {
	text := strings.TrimSpace(d.nodeSearch.Text)
	if text == "" {
		d.showError(fmt.Errorf("请输入要搜索的名称或 NodeID"))
		return
	}
	root := d.browseRoot.Text
	match := matchNode(text, d.nodeSearchMode.Selected == "精确")
	d.runCheck(func(ctx context.Context) error {
		return d.controller.SearchNodes(ctx, root, func(n device.Node) bool { return match(n.NodeID, n.DisplayName, n.BrowseName) })
	}, nil)
}

// newTextTable renders cells on demand, so only the visible rows are formatted,
// under a header row that stays in view while the rows scroll.
func newTextTable(rows func() int, header []string, cell func(row, col int) string, style func(row, col int) widget.Importance, widths []float32) *widget.Table {
	t := widget.NewTable(func() (int, int) { return rows(), len(widths) }, func() fyne.CanvasObject {
		// Long values such as Chinese text would otherwise run over the next column.
		l := widget.NewLabel("")
		l.Truncation = fyne.TextTruncateEllipsis
		return container.NewStack(canvas.NewRectangle(color.Transparent), l)
	}, func(id widget.TableCellID, o fyne.CanvasObject) {
		cell, objects := cell, o.(*fyne.Container).Objects
		// Alternate row shading keeps a long row readable across the columns.
		stripe := objects[0].(*canvas.Rectangle)
		stripe.FillColor = color.Transparent
		if id.Row%2 == 1 {
			stripe.FillColor = theme.Color(theme.ColorNameInputBackground)
		}
		stripe.Refresh()
		l := objects[1].(*widget.Label)
		l.Importance = style(id.Row, id.Col)
		l.SetText(cell(id.Row, id.Col))
	})
	t.ShowHeaderRow = true
	t.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 && id.Col < len(header) {
			o.(*widget.Label).SetText(header[id.Col])
		}
	}
	for i, w := range widths {
		t.SetColumnWidth(i, w)
	}
	return t
}

// sortableHeader makes each header a button that calls sort with its column;
// mark shows the sort direction after the title.
func sortableHeader(t *widget.Table, header []string, mark func(col int) string, sort func(col int)) {
	t.CreateHeader = func() fyne.CanvasObject {
		b := widget.NewButton("", nil)
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		return b
	}
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col < 0 || id.Col >= len(header) {
			return
		}
		b := o.(*widget.Button)
		b.SetText(header[id.Col] + mark(id.Col))
		b.OnTapped = func() { sort(id.Col) }
	}
}

var browseHeader = []string{"选择", "NodeID", "显示名称", "节点类型"}

func (d *Desktop) browseStyle(row, col int) widget.Importance {
	if col == 0 && row < len(d.snapshot.Nodes) && d.browseSelected[d.snapshot.Nodes[row].NodeID] {
		return widget.HighImportance
	}
	return widget.MediumImportance
}

func (d *Desktop) browseCell(row, col int) string {
	if row >= len(d.snapshot.Nodes) {
		return ""
	}
	n := d.snapshot.Nodes[row]
	switch col {
	case 0:
		if d.browseSelected[n.NodeID] {
			return "■"
		}
		return "□"
	case 1:
		return n.NodeID
	case 2:
		return n.DisplayName
	}
	return n.Class
}

// selectNode keeps the selection in the order nodes were chosen, across folders.
func (d *Desktop) selectNode(n device.Node, on bool) {
	if on == d.browseSelected[n.NodeID] {
		return
	}
	if on {
		d.browseSelected[n.NodeID] = true
		d.browseSelection = append(d.browseSelection, n)
		return
	}
	delete(d.browseSelected, n.NodeID)
	for i, s := range d.browseSelection {
		if s.NodeID == n.NodeID {
			d.browseSelection = append(d.browseSelection[:i], d.browseSelection[i+1:]...)
			break
		}
	}
}

func (d *Desktop) selectionChanged() {
	d.selectionLabel.SetText(fmt.Sprintf("已选 %d 个节点", len(d.browseSelection)))
	d.browseTable.Refresh()
	d.controls()
}
