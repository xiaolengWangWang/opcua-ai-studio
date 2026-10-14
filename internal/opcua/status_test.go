package opcua

import (
	"context"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
	"opcua-ai-studio/internal/device"
	"testing"
	"time"
)

func TestServerWriteTimeoutIsUnknown(t *testing.T) {
	_, _, target := testServer(t, func(s *server.Server, ns *server.NodeNameSpace) {
		s.RegisterHandler(id.WriteRequest_Encoding_DefaultBinary, func(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
			req := r.(*ua.WriteRequest)
			return &ua.WriteResponse{ResponseHeader: &ua.ResponseHeader{Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle, ServiceResult: ua.StatusGood, ServiceDiagnostics: &ua.DiagnosticInfo{}, AdditionalHeader: ua.NewExtensionObject(nil)}, Results: []ua.StatusCode{ua.StatusBadTimeout}}, nil
		})
	}, server.EnableSecurity("None", ua.MessageSecurityModeNone))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := Dial(ctx, target, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, _ := device.ParseAddress("ns=1;s=Counter", "Int32", 1)
	v, _ := device.ParseValue(a, "99")
	r, err := c.Write(ctx, a, v)
	if err == nil || r.State != "UNKNOWN" {
		t.Fatalf("timeout was reported as a definite outcome: %+v %v", r, err)
	}
}
