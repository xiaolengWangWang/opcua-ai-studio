package ui

import (
	"encoding/csv"
	"fmt"
	"io"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"strconv"
	"strings"
	"time"
)

// ExportPointsCSV exports the displayed snapshot, without performing another read.
func (d *Desktop) ExportPointsCSV(out io.Writer, filter string) error {
	return exportPointsCSV(out, d.snapshot, filter)
}

func exportPointsCSV(out io.Writer, snapshot service.Snapshot, filter string) error {
	if filter != "全部" && filter != "GOOD" && filter != "BAD" && filter != "UNCERTAIN" && filter != "NOT_READ" && filter != "未读取" && filter != "请求失败" && filter != "解析/类型错误" {
		return fmt.Errorf("未知质量筛选 %q", filter)
	}
	if _, err := io.WriteString(out, "\ufeff"); err != nil {
		return err
	}
	w := csv.NewWriter(out)
	w.UseCRLF = true
	if err := w.Write([]string{"Endpoint", "NodeID", "类型", "值", "质量", "状态码", "读取时间", "源时间", "服务器时间", "错误", "显示名称", "任务状态", "执行状态", "任务ID", "父节点范围", "范围完整"}); err != nil {
		return err
	}
	names := map[string]string{}
	for _, n := range snapshot.Nodes {
		names[n.NodeID] = n.DisplayName
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339Nano)
	}
	for _, r := range snapshot.Results {
		if !matchesPoint(r, filter) {
			continue
		}
		value := device.FormatValue(r.Value)
		if r.Value.Type == "" {
			value = ""
		}
		kind := string(r.Value.Type)
		if kind == "" {
			kind = string(r.Address.Type)
		}
		code := fmt.Sprintf("0x%08X", r.StatusCode)
		if r.Quality == "UNKNOWN" {
			code = ""
		}
		complete := ""
		name := r.DisplayName
		if name == "" {
			name = names[r.Address.NodeID]
		}
		taskStatus := ""
		if r.TaskID != "" {
			complete = strconv.FormatBool(r.Complete)
			taskStatus = "未完成"
			if r.Complete {
				taskStatus = "完成"
			}
		}
		row := []string{snapshot.Target, r.Address.NodeID, kind, value, r.Quality, code, stamp(r.Time), stamp(r.SourceTime), stamp(r.ServerTime), r.Error, name, taskStatus, r.Execution, r.TaskID, r.Scope, complete}
		for i, s := range row {
			if first := strings.TrimLeft(s, " "); first != "" && strings.ContainsRune("=+-@\t\r\n", rune(first[0])) {
				row[i] = "'" + s
			}
		}
		if (r.Value.Type == "Int64" || r.Value.Type == "UInt64") && r.Value.Elements == nil && !strings.HasPrefix(row[3], "'") {
			row[3] = "'" + row[3]
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
