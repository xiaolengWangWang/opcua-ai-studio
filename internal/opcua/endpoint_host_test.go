package opcua

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
)

func TestEndpointHostComesFromAdvertisedURL(t *testing.T) {
	_, _, target := testServer(t, func(s *server.Server, _ *server.NodeNameSpace) {
		s.RegisterHandler(id.GetEndpointsRequest_Encoding_DefaultBinary, func(_ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
			ee := s.Endpoints()
			for i, e := range ee {
				copy := *e
				copy.EndpointURL = strings.Replace(e.EndpointURL, "127.0.0.1", "admin-PC", 1)
				ee[i] = &copy
			}
			return &ua.GetEndpointsResponse{ResponseHeader: &ua.ResponseHeader{Timestamp: time.Now(), RequestHandle: r.Header().RequestHandle, ServiceResult: ua.StatusOK, ServiceDiagnostics: &ua.DiagnosticInfo{}, AdditionalHeader: ua.NewExtensionObject(nil)}, Endpoints: ee}, nil
		})
	}, server.EnableSecurity("None", ua.MessageSecurityModeNone))
	ctx := t.Context()
	ee, err := Endpoints(ctx, target, 5*time.Second)
	if err != nil || len(ee) == 0 {
		t.Fatalf("endpoints: %+v, %v", ee, err)
	}
	if ee[0].EndpointHost != "admin-PC" {
		t.Fatalf("endpoint-host = %q, want advertised admin-PC", ee[0].EndpointHost)
	}
	found := describe(ctx, target, 5*time.Second, netip.Addr{})
	if found.Error != "" || len(found.Endpoints) == 0 || found.Endpoints[0].EndpointHost != "admin-PC" {
		t.Fatalf("search endpoint-host: %+v", found)
	}
	c, err := Dial(ctx, target, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	info, err := c.ServerInfo(ctx)
	if err != nil || info.Endpoint != target || info.EndpointHost != "admin-PC" {
		t.Fatalf("server info: %+v, %v", info, err)
	}
}
