package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/ai"
	"opcua-ai-studio/internal/service"
	"os"
	"strings"
	"time"
)

func diagnosticSnapshot(s service.Snapshot) ai.Snapshot {
	shot := ai.Snapshot{ID: fmt.Sprint(time.Now().UnixNano()), Source: "OPC UA AI Studio", CapturedAt: time.Now().UTC().Format(time.RFC3339), Note: "服务级记录；时间戳来自服务端。没有提供单位、设备工程配置或线缆原始字节。"}
	add := func(kind string, v any) {
		b, err := json.Marshal(v)
		if err == nil {
			shot.Evidence = append(shot.Evidence, ai.Evidence{ID: fmt.Sprintf("E%d", len(shot.Evidence)+1), Kind: kind, Data: b})
		}
	}
	add("session", struct{ State, Target, LastError string }{s.State, s.Target, s.LastError})
	rr := s.Results
	if len(rr) > 20 {
		rr = rr[:20]
	}
	add("read_results", rr)
	if s.WriteResult != nil {
		add("write_result", s.WriteResult)
	}
	pp := s.Packets
	if len(pp) > 8 {
		pp = pp[len(pp)-8:]
	}
	add("service_events", pp)
	return shot
}
func (d *Desktop) aiPanel() fyne.CanvasObject {
	key := widget.NewPasswordEntry()
	key.SetPlaceHolder("DeepSeek API Key（仅本次会话）")
	key.SetText(os.Getenv("DEEPSEEK_API_KEY"))
	model := widget.NewEntry()
	model.SetText(ai.DefaultModel)
	question := widget.NewEntry()
	question.SetText("请分析连接状态、节点读写结果和下一步人工检查。")
	preview := widget.NewLabel("点击“冻结并预览证据”查看将发送的 Endpoint、NodeID、值与状态。")
	preview.Selectable = true
	preview.Wrapping = fyne.TextWrapBreak
	answer := widget.NewLabel("AI 只解释当前证据；设备操作由读写页执行。")
	answer.Selectable = true
	answer.Wrapping = fyne.TextWrapWord
	var frozen *ai.Snapshot
	freeze := widget.NewButton("冻结并预览证据", func() {
		s := diagnosticSnapshot(d.controller.Snapshot())
		b, err := s.JSON()
		if err != nil {
			d.showError(err)
			return
		}
		frozen = &s
		preview.SetText(string(b))
	})
	analyze := widget.NewButton("分析（发送预览内容）", func() {
		if frozen == nil {
			d.showError(fmt.Errorf("请先冻结并预览证据"))
			return
		}
		s := *frozen
		k, m, q := key.Text, model.Text, question.Text
		var response ai.Response
		d.run(func(ctx context.Context) error {
			var err error
			response, err = (&ai.Client{Key: k, Model: m}).Analyze(ctx, s, q)
			return err
		}, func() {
			var lines []string
			lines = append(lines, response.Result.Summary, "", response.Metadata())
			for _, c := range response.Result.Observations {
				lines = append(lines, "事实: "+c.Text+" ["+strings.Join(c.EvidenceIDs, ", ")+"]")
			}
			for _, c := range response.Result.Hypotheses {
				lines = append(lines, "假设: "+c.Text+" ["+strings.Join(c.EvidenceIDs, ", ")+"]")
			}
			for _, c := range response.Result.NextChecks {
				lines = append(lines, "人工检查: "+c)
			}
			answer.SetText(strings.Join(lines, "\n\n"))
		})
	})
	fields := container.NewGridWithColumns(2, widget.NewForm(widget.NewFormItem("API Key", key)), widget.NewForm(widget.NewFormItem("模型", model)))
	top := container.NewVBox(widget.NewLabel("点击分析后，预览内容将发送到 DeepSeek；密码及 API Key 不包含在证据中。"), fields, question, container.NewHBox(freeze, analyze))
	split := container.NewHSplit(container.NewVScroll(preview), container.NewVScroll(answer))
	split.SetOffset(.45)
	return container.NewBorder(top, nil, nil, nil, split)
}
