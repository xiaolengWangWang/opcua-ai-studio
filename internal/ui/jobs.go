package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

// runFile runs a file job in the background with a progress row: importing or
// exporting 50000 points must never block the window. Stop and the row's
// cancel button both cancel it; message is shown when the job succeeds.
func (d *Desktop) runFile(title string, fn func(ctx context.Context, report func(done, total int)) (message string, err error)) bool {
	return d.runJob(title, fn, nil)
}

// runJob is runFile with then called after the job succeeds.
func (d *Desktop) runJob(title string, fn func(ctx context.Context, report func(done, total int)) (message string, err error), then func()) bool {
	if d.busy != 0 {
		d.showError(errors.New("请等待当前操作完成或先停止"))
		return false
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.checkCancel = cancel
	d.jobLabel.SetText(title)
	d.jobBar.SetValue(0)
	d.jobBox.Show()
	var last time.Time
	report := func(done, total int) {
		if total <= 0 || done < total && time.Since(last) < 100*time.Millisecond {
			return
		}
		last = time.Now()
		fyne.Do(func() {
			if !d.jobFollow {
				d.jobBar.SetValue(float64(done) / float64(total))
			}
		})
	}
	var message string
	d.run(func(context.Context) error {
		defer cancel()
		var err error
		message, err = fn(ctx, report)
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("%s：已取消", title)
		}
		return err
	}, func() {
		if message != "" {
			dialog.ShowInformation("完成", message, d.win)
		}
		if then != nil {
			then()
		}
	})
	return true
}

// saveFile asks for a destination and writes it as a file job; a failed or
// cancelled write removes the partial file.
func (d *Desktop) saveFile(title, name string, write func(ctx context.Context, out io.Writer, report func(done, total int)) (string, error)) {
	save := dialog.NewFileSave(func(out fyne.URIWriteCloser, err error) {
		if err != nil {
			d.showError(err)
			return
		}
		if out == nil {
			return
		}
		started := d.runFile(title, func(ctx context.Context, report func(int, int)) (string, error) {
			buf := bufio.NewWriterSize(out, 1<<20)
			message, err := write(ctx, buf, report)
			if err == nil {
				err = buf.Flush()
			}
			if closeErr := out.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				storage.Delete(out.URI())
			}
			return message, err
		})
		if !started {
			out.Close()
			storage.Delete(out.URI())
		}
	}, d.win)
	save.SetTitleText(title)
	save.SetFileName(name)
	save.Show()
}
