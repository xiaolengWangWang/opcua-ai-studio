package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/opcua"
)

// probeSessionsDialog measures how many sessions the server holds at once with
// the current endpoint, security and credentials, after a warning: the probe
// briefly takes the server's free sessions from other clients.
func (d *Desktop) probeSessionsDialog() {
	cfg, err := d.config()
	if err != nil {
		d.showError(err)
		return
	}
	limit := widget.NewSelect([]string{"10", "50", "100", "200", "500"}, nil)
	limit.SetSelected("100")
	content := container.NewVBox(
		widget.NewLabel("按当前 Endpoint、安全策略和账号依次建立会话并保持，直到服务器拒绝或达到上限，随后全部关闭。\n测试期间可能占满服务器会话，影响 viSCADA 等其他客户端，请在测试环境或确认允许后执行。"),
		widget.NewForm(widget.NewFormItem("最多建立", limit)),
	)
	dialog.ShowCustomConfirm("连接数测试", "开始测试", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		n, _ := strconv.Atoi(limit.Selected)
		d.tabs.SelectIndex(0)
		d.runFile(fmt.Sprintf("连接数测试 · 最多 %d 个会话", n), func(ctx context.Context, report func(int, int)) (string, error) {
			open, err := opcua.ProbeSessions(ctx, cfg.Target, cfg.Options, n, func(open int) { report(open, n) })
			switch {
			case errors.Is(err, context.Canceled):
				return "", err
			case err != nil:
				return fmt.Sprintf("服务器同时接受了 %d 个会话，第 %d 个被拒绝：%v\n测试会话已全部关闭。", open, open+1, err), nil
			}
			return fmt.Sprintf("已同时建立 %d 个会话，达到测试上限，服务器未拒绝。\n测试会话已全部关闭。", open), nil
		})
	}, d.win)
}
