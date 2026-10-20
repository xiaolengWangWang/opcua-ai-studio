package main

import (
	"context"
	"flag"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"image"
	"io"
	"log"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/ui"
	"opcua-ai-studio/internal/update"
	"opcua-ai-studio/platform"
	"os"
	"path/filepath"
	"time"
)

var version = "0.6.1"

func main() {
	smokeDir := flag.String("smoke-test", "", "验证自有本机演示并保存 GUI 截图与记录后退出")
	afterUpdate := flag.Int("after-update", 0, "软件内更新后由旧进程传入其 PID：等待其退出再清理旧文件")
	flag.Parse()
	if exe, err := os.Executable(); err == nil {
		if *afterUpdate > 0 {
			waitExit(*afterUpdate, 15*time.Second)
		}
		update.Cleanup(exe)
	}
	platform.Setup()
	platform.PrewarmFonts()
	a := app.NewWithID("com.opcuaaistudio.desktop")
	platform.ConfigureApp(a)
	o := service.Options{}
	if *smokeDir != "" {
		o.RecordDir = filepath.Join(*smokeDir, "records")
	}
	c := service.New(o)
	d := ui.NewDesktop(a, c, version)
	d.Show()
	if *smokeDir == "" {
		d.CheckUpdatesInBackground()
	}
	result := make(chan error, 1)
	if *smokeDir != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			err := runSmoke(ctx, c, func() image.Image {
				time.Sleep(600 * time.Millisecond)
				var im image.Image
				fyne.DoAndWait(func() { im = d.Window().Canvas().Capture() })
				return im
			}, func(out io.Writer, filter string) error {
				var exportErr error
				fyne.DoAndWait(func() { exportErr = d.ExportPointsCSV(out, filter) })
				return exportErr
			}, func(search, view string, tab int) { fyne.DoAndWait(func() { d.ShowSearch(search, view, tab) }) }, *smokeDir)
			result <- err
			fyne.Do(a.Quit)
		}()
	}
	a.Run()
	closeErr := d.Shutdown()
	if closeErr != nil {
		log.Printf("保存记录失败: %v", closeErr)
	}
	if *smokeDir != "" {
		select {
		case err := <-result:
			if err != nil {
				log.Printf("GUI 验证失败: %v", err)
				os.Exit(1)
			}
		default:
			log.Print("GUI 验证被中断")
			os.Exit(1)
		}
	}
	if closeErr != nil {
		os.Exit(1)
	}
}

// waitExit waits for the process that started this one to exit, so its files
// are released before they are cleaned up.
func waitExit(pid int, limit time.Duration) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
	}
}
