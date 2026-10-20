package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		d.target.SetText(p.Endpoint)
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
		if u, err := url.Parse(strings.TrimSpace(d.target.Text)); err == nil {
			name.SetText(u.Host)
		}
	}
	dialog.ShowForm("保存连接配置", "保存", "取消", []*widget.FormItem{widget.NewFormItem("名称", name), widget.NewFormItem("", widget.NewLabel("保存 Endpoint、安全策略、超时、用户名、证书和自动重连；不保存密码。"))}, func(ok bool) {
		n := strings.TrimSpace(name.Text)
		if !ok || n == "" {
			return
		}
		p := profile{Name: n, Endpoint: strings.TrimSpace(d.target.Text), Policy: d.policy.Selected, Mode: d.mode.Selected, Timeout: d.timeout.Text, Username: d.username.Text, Cert: d.cert.Text, Key: d.key.Text, ServerCert: d.serverCert.Text, AutoReconnect: d.autoReconnect.Checked}
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
		d.security.Open(0)
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
// networks; choosing one fills its Endpoint and the least secure endpoint's
// policy, which anonymous demo and test servers accept.
func (d *Desktop) serverSearchDialog() {
	timeout, err := time.ParseDuration(d.timeout.Text)
	if err != nil {
		d.showError(errors.New("超时须为 5s、500ms 等格式"))
		return
	}
	hosts := widget.NewMultiLineEntry()
	hosts.SetMinRowsVisible(2)
	hosts.Wrapping = fyne.TextWrapWord
	hosts.SetText(strings.Join(opcua.LocalNetworks(), ", "))
	ports := widget.NewEntry()
	ports.SetText(opcua.DefaultPorts)
	status := widget.NewLabel("填写主机、IP、网段（如 192.168.1.0/24）或范围（如 192.168.1.10-50），逗号分隔。")
	status.Wrapping = fyne.TextWrapWord
	bar := widget.NewProgressBar()
	bar.Hide()
	var found []opcua.Found
	var box dialog.Dialog
	list := widget.NewList(func() int { return len(found) }, func() fyne.CanvasObject {
		name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		detail := widget.NewLabel("")
		detail.Importance = widget.LowImportance
		return container.NewVBox(name, detail)
	}, func(i widget.ListItemID, o fyne.CanvasObject) {
		f, labels := found[i], o.(*fyne.Container).Objects
		labels[0].(*widget.Label).SetText(fmt.Sprintf("%s  %s", or(f.Name, "OPC UA 服务器"), f.URL))
		var modes []string
		for _, e := range f.Endpoints {
			if m := e.Policy + "/" + e.Mode; !slices.Contains(modes, m) {
				modes = append(modes, m)
			}
		}
		detail := fmt.Sprintf("%d 个端点：%s", len(f.Endpoints), strings.Join(modes, "、"))
		if f.Error != "" {
			detail = "已在发现服务器登记，但无法读取端点：" + f.Error
		}
		labels[1].(*widget.Label).SetText(detail)
	})
	list.OnSelected = func(i widget.ListItemID) {
		f := found[i]
		d.target.SetText(f.URL)
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
	}
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
				d.startServerSearch(hh, pp, timeout, &cancel, search, bar, status, func(ff []opcua.Found) { found = ff; list.Refresh() })
				return
			}
		}
		status.SetText(err.Error())
	})
	search.Importance = widget.HighImportance
	form := widget.NewForm(widget.NewFormItem("主机 / 网段", hosts), widget.NewFormItem("端口", ports))
	top := container.NewVBox(form, container.NewBorder(nil, nil, nil, search, bar), status)
	box = dialog.NewCustom("搜索 OPC UA 服务器", "关闭", container.NewBorder(top, nil, nil, nil, list), d.win)
	box.SetOnClosed(func() {
		if cancel != nil {
			cancel()
		}
	})
	box.Resize(fyne.NewSize(860, 560))
	box.Show()
}

// startServerSearch runs one discovery; the button stops it while it runs.
func (d *Desktop) startServerSearch(hosts []string, ports []int, timeout time.Duration, cancel *context.CancelFunc, button *widget.Button, bar *widget.ProgressBar, status *widget.Label, done func([]opcua.Found)) {
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
	status.SetText(fmt.Sprintf("正在搜索 %d 台主机的 %d 个端口……", len(hosts), len(ports)))
	var last time.Time
	go func() {
		defer d.workers.Done()
		started := time.Now()
		ff, err := opcua.Discover(ctx, hosts, ports, timeout, func(n, total int) {
			if n < total && time.Since(last) < 100*time.Millisecond {
				return
			}
			last = time.Now()
			fyne.Do(func() { bar.SetValue(float64(n) / float64(total)) })
		})
		stop()
		fyne.Do(func() {
			*cancel = nil
			button.SetText("开始搜索")
			button.SetIcon(theme.SearchIcon())
			bar.Hide()
			switch {
			case errors.Is(err, context.Canceled):
				status.SetText("已停止搜索。")
			case err != nil:
				status.SetText(err.Error())
			default:
				status.SetText(fmt.Sprintf("找到 %d 个 OPC UA 服务器（%d 个地址，用时 %.1f 秒）。点选一个即可填入 Endpoint 和安全策略。", len(ff), total, time.Since(started).Seconds()))
				done(ff)
			}
		})
	}()
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
