package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/update"
)

// CheckUpdatesInBackground looks for a newer release without interrupting:
// a found update only shows the status-bar button.
func (d *Desktop) CheckUpdatesInBackground() { d.checkUpdates(true) }

func (d *Desktop) checkUpdates(quiet bool) {
	d.checkUpdateBtn.Disable()
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		ctx, cancel := context.WithTimeout(d.ctx, 15*time.Second)
		defer cancel()
		r, err := update.Latest(ctx, http.DefaultClient)
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			d.checkUpdateBtn.Enable()
			switch {
			case err != nil && !quiet:
				d.showError(err)
			case err != nil:
			case update.Newer(r.Version, d.version):
				d.updateBtn.SetText("新版本 v" + r.Version)
				d.updateBtn.OnTapped = func() { d.showUpdate(r) }
				d.updateBtn.Show()
				if !quiet {
					d.showUpdate(r)
				}
			case !quiet:
				dialog.ShowInformation("检查更新", fmt.Sprintf("当前 v%s 已是最新版本（%s）。", d.version, r.Source), d.win)
			}
		})
	}()
}

func (d *Desktop) showUpdate(r update.Release) {
	notes := widget.NewRichTextFromMarkdown(r.Notes)
	notes.Wrapping = fyne.TextWrapWord
	page, _ := url.Parse(r.Page)
	content := container.NewBorder(widget.NewLabel(fmt.Sprintf("当前 v%s，最新 v%s（来自 %s）。下载后按 SHA256 校验，替换程序文件并重启。", d.version, r.Version, r.Source)), widget.NewHyperlink("打开发布页", page), nil, nil, container.NewVScroll(notes))
	box := dialog.NewCustomConfirm("发现新版本 v"+r.Version, "下载并更新", "以后再说", content, func(ok bool) {
		if ok {
			d.installUpdate(r)
		}
	}, d.win)
	box.Resize(fyne.NewSize(640, 460))
	box.Show()
}

func (d *Desktop) installUpdate(r update.Release) {
	exe, err := os.Executable()
	if err != nil {
		d.showError(err)
		return
	}
	d.tabs.SelectIndex(0)
	d.runJob("下载更新 v"+r.Version, func(ctx context.Context, report func(int, int)) (string, error) {
		dir := filepath.Join(os.TempDir(), "OPCUAAIStudio-update")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		path, err := update.Download(ctx, http.DefaultClient, r, dir, func(received, total int64) { report(int(received>>10), int(total>>10)) })
		if err != nil {
			return "", err
		}
		defer os.Remove(path)
		return "", update.Install(path, exe)
	}, func() {
		dialog.ShowConfirm("更新已安装", fmt.Sprintf("已更新到 v%s，重启后生效。现在重启？", r.Version), func(ok bool) {
			if ok {
				d.restart(exe)
			}
		}, d.win)
	})
}

// restart starts the updated program, which waits for this process to exit
// before cleaning up the replaced files, then quits normally.
func (d *Desktop) restart(exe string) {
	if err := exec.Command(exe, "-after-update", strconv.Itoa(os.Getpid())).Start(); err != nil {
		d.showError(fmt.Errorf("无法启动新版本，请手动打开 %s：%w", exe, err))
		return
	}
	d.quit()
}
