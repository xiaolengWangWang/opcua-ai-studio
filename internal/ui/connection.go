package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/opcua"
)

// profile is a saved connection. Passwords are never saved.
type profile struct {
	Name, Endpoint, Policy, Mode, Timeout, Username, Cert, Key, ServerCert string
	AutoReconnect                                                          bool
}

func (d *Desktop) profiles() []profile {
	var pp []profile
	json.Unmarshal([]byte(d.app.Preferences().String("profiles")), &pp)
	return pp
}

func (d *Desktop) storeProfiles(pp []profile, selected string) {
	b, _ := json.Marshal(pp)
	d.app.Preferences().SetString("profiles", string(b))
	names := make([]string, len(pp))
	for i, p := range pp {
		names[i] = p.Name
	}
	d.profile.SetOptions(names)
	if selected == "" {
		d.profile.ClearSelected()
	} else {
		d.profile.SetSelected(selected)
	}
	d.controls()
}

func (d *Desktop) applyProfile(name string) {
	for _, p := range d.profiles() {
		if p.Name != name {
			continue
		}
		d.setEndpoint(p.Endpoint)
		d.policy.SetSelected(or(p.Policy, "None"))
		d.mode.SetSelected(or(p.Mode, "None"))
		d.timeout.SetText(or(p.Timeout, "5s"))
		// A password typed for another account must not go to this one.
		if p.Username != d.username.Text {
			d.password.SetText("")
		}
		d.username.SetText(p.Username)
		d.cert.SetText(p.Cert)
		d.key.SetText(p.Key)
		d.serverCert.SetText(p.ServerCert)
		d.autoReconnect.SetChecked(p.AutoReconnect)
	}
}

// saveProfileDialog saves the connection inputs under a name; an existing
// name is overwritten.
func (d *Desktop) saveProfileDialog() {
	name := widget.NewEntry()
	name.SetText(d.profile.Selected)
	if name.Text == "" {
		if u, err := url.Parse(normalizeEndpoint(d.endpoint())); err == nil {
			name.SetText(u.Host)
		}
	}
	dialog.ShowForm("保存连接配置", "保存", "取消", []*widget.FormItem{widget.NewFormItem("名称", name), widget.NewFormItem("", widget.NewLabel("保存 Endpoint、安全策略、超时、用户名、证书和自动重连；不保存密码。"))}, func(ok bool) {
		n := strings.TrimSpace(name.Text)
		if !ok || n == "" {
			return
		}
		p := profile{Name: n, Endpoint: normalizeEndpoint(d.endpoint()), Policy: d.policy.Selected, Mode: d.mode.Selected, Timeout: d.timeout.Text, Username: d.username.Text, Cert: d.cert.Text, Key: d.key.Text, ServerCert: d.serverCert.Text, AutoReconnect: d.autoReconnect.Checked}
		pp := slices.DeleteFunc(d.profiles(), func(q profile) bool { return q.Name == n })
		d.storeProfiles(append(pp, p), n)
	}, d.win)
}

func (d *Desktop) deleteProfile() {
	n := d.profile.Selected
	if n == "" {
		return
	}
	dialog.ShowConfirm("删除连接配置", fmt.Sprintf("删除连接配置“%s”？", n), func(ok bool) {
		if ok {
			d.storeProfiles(slices.DeleteFunc(d.profiles(), func(q profile) bool { return q.Name == n }), "")
		}
	}, d.win)
}

// endpointText describes an endpoint in one line of a pick list.
func endpointText(e opcua.Endpoint) string {
	return fmt.Sprintf("%s / %s · 认证 %s · %s", e.Policy, e.Mode, strings.Join(e.Authentication, "、"), e.URL)
}

// useEndpoint fills the security inputs from a chosen endpoint and opens the
// security section when it needs certificates.
func (d *Desktop) useEndpoint(e opcua.Endpoint) {
	d.policy.SetSelected(e.Policy)
	d.mode.SetSelected(e.Mode)
	if e.Policy != "None" {
		d.showSecurity(true)
	}
}

// endpointsDialog lists the server's endpoints; choosing one fills the
// security policy and mode. The typed Endpoint is kept: servers often report
// a host name this computer cannot resolve.
func (d *Desktop) endpointsDialog(ee []opcua.Endpoint) {
	var box dialog.Dialog
	list := widget.NewList(func() int { return len(ee) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(i widget.ListItemID, o fyne.CanvasObject) {
		o.(*widget.Label).SetText(endpointText(ee[i]))
	})
	list.OnSelected = func(i widget.ListItemID) {
		d.useEndpoint(ee[i])
		box.Hide()
	}
	hint := widget.NewLabel("点选一个端点，填入其安全策略和模式；Endpoint 保持当前输入。加密端点还须在“安全与认证”中填写证书。")
	hint.Wrapping = fyne.TextWrapWord
	box = dialog.NewCustom(fmt.Sprintf("服务器端点（%d 个）", len(ee)), "关闭", container.NewBorder(hint, nil, nil, nil, list), d.win)
	box.Resize(fyne.NewSize(820, 420))
	box.Show()
}

// serverSearchDialog finds OPC UA servers on this computer and the local
// networks. Servers show up while the scan goes on; 填入 fills the Endpoint
// and the least secure endpoint's policy, which anonymous demo and test
// servers accept, and 连接 also connects. The last hosts and ports are kept.
func (d *Desktop) serverSearchDialog() {
	timeout, err := time.ParseDuration(d.timeout.Text)
	if err != nil {
		d.showError(errors.New("超时须为 5s、500ms 等格式"))
		return
	}
	prefs := d.app.Preferences()
	hosts := widget.NewEntry()
	hosts.SetPlaceHolder("例如 192.168.1.10，或 192.168.1.10-50、192.168.1.0/24，逗号分隔")
	ports := widget.NewEntry()
	ports.SetText(prefs.StringWithFallback("scan-ports", opcua.DefaultPorts))
	// The search starts on this computer's own network: scanning through a
	// VPN or proxy adapter floods that software and can stall the computer.
	nets := opcua.LocalNetworks()
	scopes, prefixes := scopeOptions(nets)
	const custom = "指定 IP 或网段"
	scope := widget.NewSelect(append(scopes, "仅本机 127.0.0.1", custom), func(choice string) {
		switch choice {
		case custom:
			hosts.SetText(prefs.String("scan-custom-hosts"))
			hosts.Enable()
			d.win.Canvas().Focus(hosts)
		case "仅本机 127.0.0.1":
			hosts.SetText("127.0.0.1")
			hosts.Disable()
		default:
			hosts.SetText(prefixes[choice])
			hosts.Disable()
		}
	})
	if len(scopes) > 0 {
		scope.SetSelected(scopes[0])
	} else {
		scope.SetSelected("仅本机 127.0.0.1")
	}
	reset := widget.NewButtonWithIcon("恢复默认端口", theme.HistoryIcon(), func() { ports.SetText(opcua.DefaultPorts) })
	status := widget.NewLabel("默认搜索本机所在网段；选择“指定 IP 或网段”可只搜索某台主机。")
	status.Wrapping = fyne.TextWrapWord
	bar := widget.NewProgressBar()
	bar.Hide()
	var found []opcua.Found
	var box dialog.Dialog
	use := func(i int, connect bool) {
		if i < 0 || i >= len(found) {
			return
		}
		f := found[i]
		d.setEndpoint(f.URL)
		d.profile.ClearSelected()
		if len(f.Endpoints) > 0 {
			best := f.Endpoints[0]
			for _, e := range f.Endpoints {
				if e.Policy == "None" {
					best = e
					break
				}
			}
			d.useEndpoint(best)
		}
		box.Hide()
		if connect {
			d.connectBtn.OnTapped()
		}
	}
	list := widget.NewList(func() int { return len(found) }, func() fyne.CanvasObject {
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		detail := widget.NewLabel("")
		detail.Importance = widget.LowImportance
		detail.Truncation = fyne.TextTruncateEllipsis
		fill := widget.NewButton("填入", nil)
		connect := widget.NewButtonWithIcon("连接", theme.LoginIcon(), nil)
		connect.Importance = widget.HighImportance
		return container.NewBorder(nil, nil, nil, container.NewHBox(fill, connect), container.NewVBox(name, detail))
	}, func(i widget.ListItemID, o fyne.CanvasObject) {
		f, row := found[i], o.(*fyne.Container)
		texts := row.Objects[0].(*fyne.Container).Objects
		buttons := row.Objects[1].(*fyne.Container).Objects
		texts[0].(*widget.Label).SetText(fmt.Sprintf("%s  %s", or(f.Name, "OPC UA 服务器"), f.URL))
		var modes, auth []string
		for _, e := range f.Endpoints {
			if m := e.Policy + "/" + e.Mode; !slices.Contains(modes, m) {
				modes = append(modes, m)
			}
			for _, a := range e.Authentication {
				if !slices.Contains(auth, a) {
					auth = append(auth, a)
				}
			}
		}
		detail := fmt.Sprintf("%d 个端点：%s · 认证 %s", len(f.Endpoints), strings.Join(modes, "、"), strings.Join(auth, "、"))
		if f.Error != "" {
			detail = "已在发现服务器登记，但无法读取端点：" + f.Error
		}
		texts[1].(*widget.Label).SetText(detail)
		buttons[0].(*widget.Button).OnTapped = func() { use(i, false) }
		buttons[1].(*widget.Button).OnTapped = func() { use(i, true) }
	})
	list.OnSelected = func(i widget.ListItemID) { use(i, false) }
	var cancel context.CancelFunc
	var search *widget.Button
	search = widget.NewButtonWithIcon("开始搜索", theme.SearchIcon(), func() {
		if cancel != nil {
			cancel()
			return
		}
		hh, err := opcua.ParseHosts(hosts.Text)
		if err == nil {
			var pp []int
			if pp, err = opcua.ParsePorts(ports.Text); err == nil {
				if scope.Selected == custom {
					prefs.SetString("scan-custom-hosts", hosts.Text)
				}
				prefs.SetString("scan-ports", ports.Text)
				found = nil
				list.Refresh()
				d.startServerSearch(hh, pp, timeout, &cancel, search, bar, status, func(f opcua.Found) {
					// Keep the list in address order while servers arrive.
					at, _ := slices.BinarySearchFunc(found, f, func(a, b opcua.Found) int { return opcua.CompareURL(a.URL, b.URL) })
					found = slices.Insert(found, at, f)
					list.Refresh()
				})
				return
			}
		}
		status.SetText(err.Error())
	})
	search.Importance = widget.HighImportance
	form := widget.NewForm(widget.NewFormItem("搜索范围", scope), widget.NewFormItem("主机 / 网段", hosts), widget.NewFormItem("端口", ports))
	top := container.NewVBox(form, container.NewBorder(nil, nil, nil, container.NewHBox(reset, search), bar), status)
	box = dialog.NewCustom("搜索 OPC UA 服务器", "关闭", container.NewBorder(top, nil, nil, nil, list), d.win)
	box.SetOnClosed(func() {
		if cancel != nil {
			cancel()
		}
	})
	box.Resize(fyne.NewSize(900, 580))
	box.Show()
}

// scopeOptions names this computer's networks for the search range, its own
// network first; virtual adapters are marked, as they may lead into a VPN.
func scopeOptions(nets []opcua.Network) (options []string, prefixes map[string]string) {
	prefixes = map[string]string{}
	for _, n := range nets {
		label := fmt.Sprintf("网卡 %s · %s（%d 台）", n.Interface, n.Prefix, n.Hosts)
		switch {
		case n.Primary:
			label = fmt.Sprintf("本机网段 · %s · %s（%d 台）", n.Interface, n.Prefix, n.Hosts)
		case n.Virtual:
			label = fmt.Sprintf("虚拟网卡 %s · %s（%d 台，可能经过 VPN 或代理）", n.Interface, n.Prefix, n.Hosts)
		}
		options = append(options, label)
		prefixes[label] = n.Prefix
	}
	return options, prefixes
}

// startServerSearch runs one discovery, showing each server as it is found;
// the button stops it while it runs.
func (d *Desktop) startServerSearch(hosts []string, ports []int, timeout time.Duration, cancel *context.CancelFunc, button *widget.Button, bar *widget.ProgressBar, status *widget.Label, add func(opcua.Found)) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.workers.Add(1)
	d.mu.Unlock()
	ctx, stop := context.WithCancel(d.ctx)
	*cancel = stop
	button.SetText("停止搜索")
	button.SetIcon(theme.MediaStopIcon())
	bar.SetValue(0)
	bar.Show()
	total := len(hosts) * len(ports)
	// Connections are paced and closed ports wait for the connect timeout,
	// so the worst case is known in advance.
	estimate := max(float64(total)/opcua.DialRate, float64(total)/opcua.ScanWorkers*opcua.ConnectTimeout.Seconds())
	status.SetText(fmt.Sprintf("正在搜索 %d 台主机的 %d 个端口，共 %d 个地址，最长约 %.0f 秒……", len(hosts), len(ports), total, estimate))
	var last time.Time
	count := 0
	go func() {
		defer d.workers.Done()
		started := time.Now()
		ff, err := opcua.Discover(ctx, hosts, ports, timeout, func(n, total int) {
			if n < total && time.Since(last) < 100*time.Millisecond {
				return
			}
			last = time.Now()
			fyne.Do(func() {
				bar.SetValue(float64(n) / float64(total))
				status.SetText(fmt.Sprintf("已扫描 %d / %d 个地址，找到 %d 个服务器……", n, total, count))
			})
		}, func(f opcua.Found) {
			fyne.Do(func() {
				count++
				add(f)
			})
		})
		stop()
		fyne.Do(func() {
			*cancel = nil
			button.SetText("开始搜索")
			button.SetIcon(theme.SearchIcon())
			bar.Hide()
			switch {
			case errors.Is(err, context.Canceled):
				status.SetText(fmt.Sprintf("已停止搜索，找到 %d 个服务器。", count))
			case err != nil:
				status.SetText(err.Error())
			default:
				status.SetText(fmt.Sprintf("找到 %d 个 OPC UA 服务器（%d 个地址，用时 %.1f 秒）。点“连接”直接连接，点“填入”只填入 Endpoint 和安全策略。", len(ff), total, time.Since(started).Seconds()))
			}
		})
	}()
}

// normalizeEndpoint completes a typed address: opc.tcp:// when the scheme is
// missing and port 4840 when the port is, without surrounding spaces.
func normalizeEndpoint(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.Contains(s, "://") {
		s = "opc.tcp://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "opc.tcp" || u.Hostname() == "" || u.Port() != "" {
		return s
	}
	u.Host = net.JoinHostPort(u.Hostname(), "4840")
	return u.String()
}

// endpointHistory lists the endpoints connected most recently, newest first.
func (d *Desktop) endpointHistory() []string {
	return strings.FieldsFunc(d.app.Preferences().String("endpoint-history"), func(r rune) bool { return r == '\n' })
}

// rememberEndpoint puts a connected endpoint first in the history of ten.
func (d *Desktop) rememberEndpoint(target string) {
	list := append([]string{target}, slices.DeleteFunc(d.endpointHistory(), func(s string) bool { return s == target })...)
	list = list[:min(len(list), 10)]
	d.app.Preferences().SetString("endpoint-history", strings.Join(list, "\n"))
	d.host.SetOptions(historyOptions(list))
}

// historyOptions shows recent Endpoints as host:port, which the address field
// splits back into the address and port when one is chosen.
func historyOptions(list []string) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = strings.TrimPrefix(e, "opc.tcp://")
	}
	return out
}

// serverInfoText formats the connected server's information for reading and copying.
func serverInfoText(info opcua.ServerInfo) string {
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Local().Format("2006-01-02 15:04:05")
	}
	lines := []string{
		"Endpoint：" + info.Endpoint,
		fmt.Sprintf("安全：%s / %s", info.Policy, info.Mode),
		"产品：" + or(info.Product, "—"),
		"厂商：" + or(info.Manufacturer, "—"),
		fmt.Sprintf("版本：%s  构建：%s（%s）", or(info.Version, "—"), or(info.Build, "—"), stamp(info.BuildDate)),
		"产品 URI：" + or(info.ProductURI, "—"),
		"状态：" + or(info.State, "—"),
		"启动时间：" + stamp(info.StartTime),
		"服务器时间：" + stamp(info.CurrentTime),
	}
	if info.Sessions >= 0 {
		lines = append(lines, fmt.Sprintf("会话诊断：当前会话 %d · 累计 %d · 拒绝 %d · 当前订阅 %d", info.Sessions, info.CumulatedSessions, info.RejectedSessions, info.Subscriptions))
	} else {
		lines = append(lines, "会话诊断：服务器未提供或未启用")
	}
	if len(info.Clients) > 0 {
		lines = append(lines, "", fmt.Sprintf("已连接客户端（%d）：", len(info.Clients)))
		for _, c := range info.Clients {
			lines = append(lines, fmt.Sprintf("  %s · 会话 %s · 连接于 %s", or(c.Application, "未知客户端"), or(c.Name, "—"), stamp(c.Connected)))
		}
	}
	lines = append(lines, "", fmt.Sprintf("命名空间（%d）：", len(info.Namespaces)))
	for i, ns := range info.Namespaces {
		lines = append(lines, fmt.Sprintf("  ns=%d  %s", i, ns))
	}
	return strings.Join(lines, "\n")
}

// serverInfoDialog reads the server information again and shows it.
func (d *Desktop) serverInfoDialog() {
	d.run(d.controller.RefreshServerInfo, func() {
		s := d.controller.Snapshot()
		if s.Server == nil {
			return
		}
		text := serverInfoText(*s.Server)
		label := widget.NewLabel(text)
		label.Selectable = true
		label.Wrapping = fyne.TextWrapWord
		copy := widget.NewButtonWithIcon("复制", theme.ContentCopyIcon(), func() { d.win.Clipboard().SetContent(text) })
		box := dialog.NewCustom("服务器信息", "关闭", container.NewBorder(nil, container.NewHBox(copy), nil, nil, container.NewVScroll(label)), d.win)
		box.Resize(fyne.NewSize(760, 560))
		box.Show()
	})
}
