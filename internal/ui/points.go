package ui

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ExportPointsCSV exports the displayed snapshot with the filter and the
// shown search, without performing another read.
func (d *Desktop) ExportPointsCSV(out io.Writer, filter string) error {
	q := pointQuery{filter: filter}
	if d.search != nil {
		shown := d.query()
		q.search, q.exact = shown.search, shown.exact
	}
	return exportPointsCSV(context.Background(), out, d.snapshot, q, nil)
}

// ShowSearch sets the result search and shows the table or the tree, as the
// smoke test captures them; a tab index of 2 shows the communication tab.
func (d *Desktop) ShowSearch(search string, tree bool, tab int) {
	d.search.SetText(search)
	d.viewMode.SetSelected(map[bool]string{false: "表格", true: "树状"}[tree])
	d.tabs.SelectIndex(tab)
}

// exportPointsCSV writes the rows matching q; done reports the rows written
// and ctx cancels a long export.
func exportPointsCSV(ctx context.Context, out io.Writer, snapshot service.Snapshot, q pointQuery, done func(int)) error {
	if !slices.Contains(filters, q.filter) && q.filter != "NOT_READ" {
		return fmt.Errorf("未知质量筛选 %q", q.filter)
	}
	keep := q.matcher()
	if _, err := io.WriteString(out, "\ufeff"); err != nil {
		return err
	}
	w := csv.NewWriter(out)
	w.UseCRLF = true
	if err := w.Write([]string{"Endpoint", "NodeID", "类型", "值", "质量", "状态码", "读取时间", "源时间", "服务器时间", "错误", "显示名称", "任务状态", "执行状态", "任务ID", "父节点范围", "范围完整", "读写权限"}); err != nil {
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
	written := 0
	for i := range snapshot.Results {
		r := &snapshot.Results[i]
		if !keep(r) {
			continue
		}
		if written%2000 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if done != nil {
				done(written)
			}
		}
		written++
		value := device.FormatValue(r.Value)
		if r.Value.Type == "" {
			value = ""
		}
		kind := string(r.Kind())
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
		row := []string{snapshot.Target, r.Address.NodeID, kind, value, r.Quality, code, stamp(r.Time), stamp(r.SourceTime), stamp(r.ServerTime), r.Error, name, taskStatus, r.Execution, r.TaskID, r.Scope, complete, accessNames[r.Access]}
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
