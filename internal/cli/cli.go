package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/recorder"
	"os"
)

func Run(ctx context.Context, args []string, out, errout io.Writer) (code int) {
	f := flag.NewFlagSet("opcua-cli", flag.ContinueOnError)
	f.SetOutput(errout)
	o := opcua.DefaultOptions()
	var target, kind, record string
	var count int
	var jsonOut, allowWrite bool
	f.StringVar(&target, "endpoint", "", "OPC TCP Endpoint，如 opc.tcp://127.0.0.1:4840")
	f.StringVar(&target, "target", "", "-endpoint 的别名")
	f.DurationVar(&o.Timeout, "timeout", o.Timeout, "连接及请求超时")
	f.StringVar(&o.Policy, "security-policy", o.Policy, "None / Basic256Sha256 / Aes128_Sha256_RsaOaep / Aes256_Sha256_RsaPss")
	f.StringVar(&o.Mode, "security-mode", o.Mode, "None / Sign / SignAndEncrypt")
	f.StringVar(&o.Username, "username", "", "用户名；密码从 OPCUA_PASSWORD 环境变量读取")
	f.StringVar(&o.Certificate, "cert", "", "客户端证书文件")
	f.StringVar(&o.PrivateKey, "key", "", "客户端私钥文件")
	f.StringVar(&o.ServerCertificate, "server-cert", "", "固定信任的服务器证书文件")
	f.StringVar(&kind, "type", "Auto", "OPC UA 数据类型；读取默认 Auto，写入须明确指定")
	f.IntVar(&count, "count", 1, "写入数组元素数量")
	f.StringVar(&record, "record", "", "SQLite 记录目录")
	f.BoolVar(&jsonOut, "json", false, "输出 JSON")
	f.BoolVar(&allowWrite, "allow-write", false, "明确允许本次写入")
	f.Usage = func() {
		fmt.Fprintln(errout, "用法：opcua-cli [参数] endpoints | browse [NodeID] | read NodeID... | write NodeID 值\n所有参数放在操作前；数组值使用 JSON，例如 [1,2,3]。")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if f.NArg() == 0 {
		f.Usage()
		return 2
	}
	if err := opcua.ValidateEndpoint(target); err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	command := f.Arg(0)
	tail := f.Args()[1:]
	if command == "endpoints" {
		if len(tail) != 0 {
			fmt.Fprintln(errout, "endpoints 不接受 NodeID")
			return 2
		}
		ee, err := opcua.Endpoints(ctx, target, o.Timeout)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if jsonOut {
			return encode(out, errout, ee)
		}
		for _, e := range ee {
			fmt.Fprintf(out, "%s  %s / %s  %v\n", e.URL, e.Policy, e.Mode, e.Authentication)
		}
		return 0
	}
	var aa []device.Address
	var value device.Value
	node := "i=85"
	switch command {
	case "browse":
		if len(tail) > 1 {
			fmt.Fprintln(errout, "browse 仅接受一个父节点")
			return 2
		}
		if len(tail) == 1 {
			node = tail[0]
		}
		if _, err := device.ParseAddress(node, "Auto", 1); err != nil {
			fmt.Fprintln(errout, err)
			return 2
		}
	case "read", "write":
		if len(tail) == 0 || (command == "write" && len(tail) != 2) {
			fmt.Fprintln(errout, "read 须提供 NodeID；write 须提供 NodeID 和值")
			return 2
		}
		ids := tail
		if command == "write" {
			ids = tail[:1]
			if !allowWrite {
				fmt.Fprintln(errout, "写入默认禁用；须提供 -allow-write")
				return 2
			}
		}
		for _, s := range ids {
			a, err := device.ParseAddress(s, device.DataType(kind), count)
			if err != nil {
				fmt.Fprintln(errout, err)
				return 2
			}
			aa = append(aa, a)
		}
		if command == "write" {
			var err error
			value, err = device.ParseValue(aa[0], tail[1])
			if err != nil {
				fmt.Fprintln(errout, err)
				return 2
			}
		}
	default:
		fmt.Fprintln(errout, "未知操作:", command)
		return 2
	}
	if o.Username != "" {
		o.Password = os.Getenv("OPCUA_PASSWORD")
	}
	if err := o.Validate(); err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	var rec *recorder.Recorder
	var session int64
	if record != "" {
		var err error
		rec, err = recorder.OpenDaily(record)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		defer func() {
			if session != 0 {
				if err := rec.EndSession(session); err != nil {
					fmt.Fprintln(errout, err)
					code = 1
				}
			}
			if err := rec.Close(); err != nil {
				fmt.Fprintln(errout, err)
				code = 1
			}
		}()
		session, err = rec.StartSession(device.ProtocolOPCUA, target, 1)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		o.Observer = device.ObserverFunc(func(e device.Event) { rec.Record(session, e) })
	}
	c, err := opcua.Dial(ctx, target, o)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	defer c.Close()
	switch command {
	case "browse":
		nn, err := c.Browse(ctx, node)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if jsonOut {
			return encode(out, errout, nn)
		}
		for _, n := range nn {
			fmt.Fprintf(out, "%s\t%s\t%s\n", n.NodeID, n.Class, n.DisplayName)
		}
	case "read":
		rr, err := c.Read(ctx, aa)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		for _, r := range rr {
			if r.Quality != "GOOD" {
				code = 1
			}
		}
		if jsonOut {
			if encode(out, errout, rr) != 0 {
				return 1
			}
		} else {
			for _, r := range rr {
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", r.Address.NodeID, r.Value.Type, device.FormatValue(r.Value), r.Quality, r.Error)
			}
		}
	case "write":
		wr, writeErr := c.Write(ctx, aa[0], value)
		result := struct {
			Write    device.WriteResult `json:"write"`
			Readback *device.ReadResult `json:"readback,omitempty"`
			Verified bool               `json:"verified"`
		}{Write: wr}
		if wr.State == "CONFIRMED" {
			rr, e := c.Read(ctx, aa)
			if e != nil {
				err = e
			} else if len(rr) == 1 {
				result.Readback = &rr[0]
				result.Verified = rr[0].Quality == "GOOD" && device.ValueEqual(value, rr[0].Value)
			}
		}
		if jsonOut {
			if encode(out, errout, result) != 0 {
				return 1
			}
		} else {
			fmt.Fprintf(out, "%s %s; 读回一致=%t\n", aa[0].NodeID, wr.State, result.Verified)
		}
		if writeErr != nil {
			fmt.Fprintln(errout, writeErr)
		}
		if err != nil {
			fmt.Fprintln(errout, err)
		}
		if writeErr != nil || err != nil || !result.Verified {
			return 1
		}
	}
	return code
}
func encode(out, errout io.Writer, v any) int {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	return 0
}
