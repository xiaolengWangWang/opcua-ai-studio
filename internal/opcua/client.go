// Package opcua adapts the OPC UA SDK to Studio's typed device interface.
package opcua

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	sdk "github.com/gopcua/opcua"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"
	"net/url"
	"opcua-ai-studio/internal/device"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Timeout                                                                      time.Duration
	Policy, Mode, Username, Password, Certificate, PrivateKey, ServerCertificate string
	Observer                                                                     device.Observer
}

// MaxNodesPerRequest bounds one request; servers that accept less reject the
// request with BadTooManyOperations and the caller retries smaller batches.
const MaxNodesPerRequest = 10000

func DefaultOptions() Options { return Options{Timeout: 5 * time.Second, Policy: "None", Mode: "None"} }
func ValidateEndpoint(target string) error {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "opc.tcp" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("Endpoint 须为明确的 opc.tcp://host:port[/path]，且不能包含密码")
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("Endpoint 端口须为 1..65535")
	}
	return nil
}
func (o Options) Validate() error {
	if o.Timeout <= 0 || o.Timeout > 5*time.Minute {
		return fmt.Errorf("超时须大于零且不超过 5 分钟")
	}
	if o.Policy != "None" && o.Policy != "Basic256Sha256" && o.Policy != "Aes128_Sha256_RsaOaep" && o.Policy != "Aes256_Sha256_RsaPss" {
		return fmt.Errorf("不支持的安全策略 %q", o.Policy)
	}
	if o.Mode != "None" && o.Mode != "Sign" && o.Mode != "SignAndEncrypt" {
		return fmt.Errorf("不支持的安全模式 %q", o.Mode)
	}
	if (o.Policy == "None") != (o.Mode == "None") {
		return fmt.Errorf("None 策略须配合 None 模式；加密策略须配合 Sign 或 SignAndEncrypt")
	}
	if o.Policy != "None" && (o.Certificate == "" || o.PrivateKey == "" || o.ServerCertificate == "") {
		return fmt.Errorf("安全连接须提供客户端证书、私钥和可信服务器证书")
	}
	if o.Username == "" && o.Password != "" {
		return fmt.Errorf("密码须配合用户名")
	}
	return nil
}

type Endpoint struct {
	URL            string   `json:"url"`
	Policy         string   `json:"policy"`
	Mode           string   `json:"mode"`
	Authentication []string `json:"authentication"`
}

func Endpoints(ctx context.Context, target string, timeout time.Duration) ([]Endpoint, error) {
	if err := ValidateEndpoint(target); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("超时须大于零")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ee, err := sdk.GetEndpoints(ctx, target, sdk.DialTimeout(timeout), sdk.RequestTimeout(timeout))
	if err != nil {
		return nil, err
	}
	out := make([]Endpoint, 0, len(ee))
	for _, e := range ee {
		v := Endpoint{URL: e.EndpointURL, Policy: strings.TrimPrefix(e.SecurityPolicyURI, ua.SecurityPolicyURIPrefix), Mode: modeName(e.SecurityMode)}
		for _, a := range e.UserIdentityTokens {
			v.Authentication = append(v.Authentication, a.TokenType.String())
		}
		out = append(out, v)
	}
	return out, nil
}
func modeName(m ua.MessageSecurityMode) string {
	switch m {
	case ua.MessageSecurityModeNone:
		return "None"
	case ua.MessageSecurityModeSign:
		return "Sign"
	case ua.MessageSecurityModeSignAndEncrypt:
		return "SignAndEncrypt"
	}
	return "Invalid"
}

type Client struct {
	client     *sdk.Client
	options    Options
	closed     atomic.Bool
	seq        atomic.Uint64
	connection string
	target     string
}

func Dial(ctx context.Context, target string, o Options) (*Client, error) {
	if err := ValidateEndpoint(target); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	ee, err := sdk.GetEndpoints(ctx, target, sdk.DialTimeout(o.Timeout), sdk.RequestTimeout(o.Timeout))
	if err != nil {
		return nil, err
	}
	auth := ua.UserTokenTypeAnonymous
	if o.Username != "" {
		auth = ua.UserTokenTypeUserName
	}
	var chosen *ua.EndpointDescription
	for _, e := range ee {
		if strings.TrimPrefix(e.SecurityPolicyURI, ua.SecurityPolicyURIPrefix) != o.Policy || modeName(e.SecurityMode) != o.Mode {
			continue
		}
		for _, a := range e.UserIdentityTokens {
			if a.TokenType == auth {
				chosen = e
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("服务器未提供所选安全策略 %s / %s 或认证方式", o.Policy, o.Mode)
	}
	if o.ServerCertificate != "" {
		trusted, e := os.ReadFile(o.ServerCertificate)
		if e != nil {
			return nil, fmt.Errorf("无法读取可信服务器证书: %w", e)
		}
		if p, _ := pem.Decode(trusted); p != nil {
			trusted = p.Bytes
		}
		if !bytes.Equal(trusted, chosen.ServerCertificate) {
			return nil, fmt.Errorf("服务器证书与可信证书不一致")
		}
	}
	opts := []sdk.Option{sdk.ApplicationName("OPC UA AI Studio"), sdk.ApplicationURI("urn:opcua-ai-studio:client"), sdk.AutoReconnect(false), sdk.RequestTimeout(o.Timeout), sdk.DialTimeout(o.Timeout)}
	if o.Username != "" {
		opts = append(opts, sdk.AuthUsername(o.Username, o.Password))
	} else {
		opts = append(opts, sdk.AuthAnonymous())
	}
	opts = append(opts, sdk.SecurityFromEndpoint(chosen, auth))
	if o.Certificate != "" {
		opts = append(opts, sdk.CertificateFile(o.Certificate))
	}
	if o.PrivateKey != "" {
		opts = append(opts, sdk.PrivateKeyFile(o.PrivateKey))
	}
	c, err := sdk.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	if err = c.Connect(ctx); err != nil {
		closeCtx, stop := context.WithTimeout(context.Background(), o.Timeout)
		defer stop()
		c.Close(closeCtx)
		return nil, err
	}
	return &Client{client: c, options: o, connection: strconv.FormatInt(time.Now().UnixNano(), 36), target: target}, nil
}
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.options.Timeout)
	defer cancel()
	return c.client.Close(ctx)
}
func (c *Client) IsClosed() bool { return c.closed.Load() || c.client.State() != sdk.Connected }
func (c *Client) event(seq uint64, dir device.Direction, op string, targets []device.Address, results []device.EventResult, start time.Time, err error) {
	if c.options.Observer == nil {
		return
	}
	status := device.StatusSuccess
	if dir == device.DirTX {
		status = device.StatusSent
	}
	msg := ""
	if err != nil {
		status = device.StatusException
		msg = err.Error()
	}
	if dir == device.DirRX {
		for _, r := range results {
			if r.Read != nil && (r.Read.Quality == "BAD" || r.Read.Error != "") {
				status = device.StatusException
			}
			if r.Write != nil && r.Write.State != "CONFIRMED" {
				status = device.StatusException
			}
		}
	}
	c.options.Observer.OnEvent(device.Event{Time: time.Now(), Protocol: device.ProtocolOPCUA, ConnectionID: c.connection, RequestID: seq, Dir: dir, Operation: op, Targets: targets, Results: results, Status: status, RTT: time.Since(start), Error: msg})
}
func (c *Client) Read(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	return c.read(ctx, aa, false)
}

// ReadWithAttributes also reads each variable's declared data type and this
// session's access in the same request, sparing large lists a second pass.
func (c *Client) ReadWithAttributes(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	return c.read(ctx, aa, true)
}

func (c *Client) read(ctx context.Context, aa []device.Address, describe bool) ([]device.ReadResult, error) {
	if len(aa) < 1 || len(aa) > MaxNodesPerRequest {
		return nil, fmt.Errorf("须读取 1..%d 个节点", MaxNodesPerRequest)
	}
	per := 1
	if describe {
		per = 3
	}
	req := &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnBoth, NodesToRead: make([]*ua.ReadValueID, 0, per*len(aa))}
	for _, a := range aa {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		n, _ := ua.ParseNodeID(a.NodeID)
		req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: n, AttributeID: ua.AttributeIDValue})
		if describe {
			req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: n, AttributeID: ua.AttributeIDDataType}, &ua.ReadValueID{NodeID: n, AttributeID: ua.AttributeIDUserAccessLevel})
		}
	}
	seq := c.seq.Add(1)
	start := time.Now()
	c.event(seq, device.DirTX, "READ", aa, nil, start, nil)
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	res, err := c.client.Read(ctx, req)
	if err != nil {
		c.event(seq, device.DirRX, "READ", aa, nil, start, err)
		return nil, err
	}
	if len(res.Results) != per*len(aa) {
		return nil, fmt.Errorf("服务器读取结果数量不一致")
	}
	rr := make([]device.ReadResult, len(aa))
	events := make([]device.EventResult, len(aa))
	for i := range aa {
		r := res.Results[per*i]
		v := device.ReadResult{Address: aa[i], Time: time.Now(), Quality: "GOOD", Execution: "READ"}
		if describe {
			v.NodeAttributes = attributes(res.Results[per*i+1], res.Results[per*i+2])
		}
		if r == nil {
			v.Quality = "UNKNOWN"
			v.Execution = "REQUEST_FAILED"
			v.Error = "服务器返回空 DataValue"
		} else {
			v.StatusCode = uint32(r.Status)
			v.SourceTime = r.SourceTimestamp
			v.ServerTime = r.ServerTimestamp
			switch uint32(r.Status) & 0xC0000000 {
			case 0x80000000:
				v.Quality = "BAD"
				v.Error = r.Status.Error()
			case 0x40000000:
				v.Quality = "UNCERTAIN"
			}
			if r.Value != nil {
				v.Value, err = device.FromNative(r.Value.Value())
				if err != nil {
					v.Execution = "PARSE_FAILED"
					if v.Error != "" {
						v.Error += "; "
					}
					v.Error += err.Error()
				}
			}
			if v.Quality != "BAD" && v.Execution == "READ" && aa[i].Type != "Auto" && v.Value.Type != aa[i].Type {
				v.Execution = "TYPE_MISMATCH"
				v.Error = fmt.Sprintf("节点类型为 %s，所选类型为 %s", v.Value.Type, aa[i].Type)
			}
		}
		rr[i] = v
		events[i].Read = &rr[i]
	}
	c.event(seq, device.DirRX, "READ", aa, events, start, nil)
	return rr, nil
}
func (c *Client) Write(ctx context.Context, a device.Address, v device.Value) (device.WriteResult, error) {
	r := device.WriteResult{Address: a, State: "REJECTED"}
	if err := a.Validate(); err != nil {
		r.Error = err.Error()
		return r, err
	}
	if a.Type == "Auto" || a.Type != v.Type {
		return r, fmt.Errorf("写入须指定匹配的 OPC UA 类型")
	}
	native, err := v.Native()
	if err != nil {
		return r, err
	}
	value, err := ua.NewVariant(native)
	if err != nil {
		return r, err
	}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	n, _ := ua.ParseNodeID(a.NodeID)
	req := &ua.WriteRequest{NodesToWrite: []*ua.WriteValue{{NodeID: n, AttributeID: ua.AttributeIDValue, Value: &ua.DataValue{EncodingMask: ua.DataValueValue, Value: value}}}}
	seq := c.seq.Add(1)
	start := time.Now()
	c.event(seq, device.DirTX, "WRITE", []device.Address{a}, nil, start, nil)
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	res, err := c.client.Write(ctx, req)
	if err != nil {
		r.State = "UNKNOWN"
		var code ua.StatusCode
		if errors.As(err, &code) {
			r.StatusCode = uint32(code)
			if writeState(code) == "REJECTED" {
				r.State = "REJECTED"
			}
		}
		r.Error = err.Error()
	} else if len(res.Results) != 1 {
		r.State = "UNKNOWN"
		err = fmt.Errorf("服务器写入结果数量不一致")
		r.Error = err.Error()
	} else {
		r.StatusCode = uint32(res.Results[0])
		r.State = writeState(res.Results[0])
		if r.State != "CONFIRMED" {
			r.Error = res.Results[0].Error()
			err = res.Results[0]
		}
	}
	c.event(seq, device.DirRX, "WRITE", []device.Address{a}, []device.EventResult{{Write: &r}}, start, err)
	return r, err
}

func writeState(code ua.StatusCode) string {
	if uint32(code)&0xC0000000 == 0 {
		return "CONFIRMED"
	}
	if uint32(code)&0xC0000000 != 0x80000000 {
		return "UNKNOWN"
	}
	switch code {
	case ua.StatusBadTimeout, ua.StatusBadCommunicationError, ua.StatusBadUnexpectedError, ua.StatusBadInternalError, ua.StatusBadConnectionClosed, ua.StatusBadSecureChannelClosed, ua.StatusBadSessionClosed, ua.StatusBadRequestInterrupted, ua.StatusBadRequestCancelledByClient:
		return "UNKNOWN"
	}
	return "REJECTED"
}
func (c *Client) Browse(ctx context.Context, node string) ([]device.Node, error) {
	out, err := c.BrowseNodes(ctx, []string{node})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// BrowseNodes lists the hierarchical children of several nodes in one request,
// following continuation points until every list is complete.
func (c *Client) BrowseNodes(ctx context.Context, nodes []string) (out [][]device.Node, err error) {
	if len(nodes) < 1 || len(nodes) > MaxNodesPerRequest {
		return nil, fmt.Errorf("须浏览 1..%d 个节点", MaxNodesPerRequest)
	}
	aa := make([]device.Address, len(nodes))
	// The SDK closes the session on a response above the negotiated message
	// size (2 MB on many servers), so each page holds at most 5000 references,
	// about 0.5 MB; continuation points fetch the rest of large folders.
	req := &ua.BrowseRequest{RequestedMaxReferencesPerNode: uint32(min(1000, max(1, 5000/len(nodes))))}
	for i, node := range nodes {
		if aa[i], err = device.ParseAddress(node, "Auto", 1); err != nil {
			return nil, err
		}
		n, _ := ua.ParseNodeID(aa[i].NodeID)
		req.NodesToBrowse = append(req.NodesToBrowse, &ua.BrowseDescription{NodeID: n, BrowseDirection: ua.BrowseDirectionForward, ReferenceTypeID: ua.NewNumericNodeID(0, id.HierarchicalReferences), IncludeSubtypes: true, ResultMask: uint32(ua.BrowseResultMaskAll)})
	}
	seq := c.seq.Add(1)
	start := time.Now()
	c.event(seq, device.DirTX, "BROWSE", aa, nil, start, nil)
	defer func() { c.event(seq, device.DirRX, "BROWSE", aa, nil, start, err) }()
	// Each page has its own timeout: a folder of 50000 variables spans many pages.
	reqCtx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	res, err := c.client.Browse(reqCtx, req)
	cancel()
	if err != nil {
		return nil, err
	}
	pages := res.Results
	if len(pages) != len(nodes) {
		return nil, fmt.Errorf("服务器浏览结果数量不一致")
	}
	out = make([][]device.Node, len(nodes))
	owner := make([]int, len(nodes))
	for i := range owner {
		owner[i] = i
	}
	var pending [][]byte
	defer func() {
		if len(pending) > 0 {
			releaseCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			c.client.BrowseNext(releaseCtx, &ua.BrowseNextRequest{ReleaseContinuationPoints: true, ContinuationPoints: pending})
		}
	}()
	for round := 0; ; round++ {
		var nextOwner []int
		for i, page := range pages {
			if page != nil && len(page.ContinuationPoint) > 0 {
				pending = append(pending, page.ContinuationPoint)
				nextOwner = append(nextOwner, owner[i])
			}
		}
		for i, page := range pages {
			if page == nil {
				return nil, fmt.Errorf("服务器返回空浏览页")
			}
			if uint32(page.StatusCode)&0xC0000000 != 0 {
				return nil, page.StatusCode
			}
			list := out[owner[i]]
			for _, ref := range page.References {
				if ref == nil || ref.NodeID == nil || ref.NodeID.NodeID == nil {
					continue
				}
				x := device.Node{NodeID: device.FormatNodeID(ref.NodeID.NodeID), Class: ref.NodeClass.String()}
				if ref.BrowseName != nil {
					x.BrowseName = ref.BrowseName.Name
				}
				if ref.DisplayName != nil {
					x.DisplayName = ref.DisplayName.Text
				}
				list = append(list, x)
			}
			if len(list) > 100000 {
				return nil, fmt.Errorf("单个节点的子节点超过 100000 个，请选择较小的父节点")
			}
			out[owner[i]] = list
		}
		if len(pending) == 0 {
			return out, nil
		}
		if round >= 9999 {
			return nil, fmt.Errorf("浏览分页过多，请选择较小的父节点")
		}
		pageCtx, stop := context.WithTimeout(ctx, c.options.Timeout)
		next, e := c.client.BrowseNext(pageCtx, &ua.BrowseNextRequest{ContinuationPoints: pending})
		stop()
		if e != nil {
			return nil, e
		}
		if len(next.Results) != len(pending) {
			return nil, fmt.Errorf("服务器浏览分页结果数量不一致")
		}
		pages, owner, pending = next.Results, nextOwner, nil
	}
}

// builtinTypes maps namespace-zero data types to the types Studio reads;
// IntegerId, Counter, Duration and UtcTime are encoded as their base types.
var builtinTypes = map[uint32]device.DataType{id.Boolean: "Boolean", id.SByte: "SByte", id.Byte: "Byte", id.Int16: "Int16", id.UInt16: "UInt16", id.Int32: "Int32", id.UInt32: "UInt32", id.Int64: "Int64", id.UInt64: "UInt64", id.Float: "Float", id.Double: "Double", id.String: "String", id.DateTime: "DateTime", id.ByteString: "ByteString", id.IntegerID: "UInt32", id.Counter: "UInt32", id.Duration: "Double", id.UtcTime: "DateTime"}

// attributes reads a variable's DataType and UserAccessLevel results. The
// type is "" for structures, enumerations and other types without one
// built-in encoding; the access is "" when the server does not report it.
func attributes(dataType, access *ua.DataValue) (out device.NodeAttributes) {
	value := func(r *ua.DataValue) any {
		if r == nil || r.Value == nil || uint32(r.Status)&0x80000000 != 0 {
			return nil
		}
		return r.Value.Value()
	}
	var n *ua.NodeID
	switch v := value(dataType).(type) {
	case *ua.NodeID:
		n = v
	case *ua.ExpandedNodeID:
		n = v.NodeID
	}
	if n != nil && n.Namespace() == 0 && n.Type() != ua.NodeIDTypeString && n.Type() != ua.NodeIDTypeGUID && n.Type() != ua.NodeIDTypeByteString {
		out.DataType = builtinTypes[n.IntID()]
	}
	if level, ok := value(access).(uint8); ok {
		read, write := level&uint8(ua.AccessLevelTypeCurrentRead) != 0, level&uint8(ua.AccessLevelTypeCurrentWrite) != 0
		switch {
		case read && write:
			out.Access = "READ_WRITE"
		case read:
			out.Access = "READ"
		case write:
			out.Access = "WRITE"
		default:
			out.Access = "NONE"
		}
	}
	return out
}

// ProbeSessions opens sessions one after another and keeps them open until the
// server refuses one or limit are open, then closes them all. It reports how
// many sessions the server held at once and the refusal, if any.
func ProbeSessions(ctx context.Context, target string, o Options, limit int, progress func(open int)) (int, error) {
	o.Observer = nil
	var open []*Client
	defer func() {
		var wg sync.WaitGroup
		for _, c := range open {
			wg.Go(func() { c.Close() })
		}
		wg.Wait()
	}()
	for len(open) < limit {
		if err := ctx.Err(); err != nil {
			return len(open), err
		}
		c, err := Dial(ctx, target, o)
		if err != nil {
			return len(open), err
		}
		open = append(open, c)
		if progress != nil {
			progress(len(open))
		}
	}
	return len(open), nil
}
