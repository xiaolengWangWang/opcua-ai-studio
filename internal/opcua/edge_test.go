package opcua

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"
	"math/big"
	"net"
	"net/url"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/simulator"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowseFailureHasAResponseRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var events []device.Event
	o := DefaultOptions()
	o.Observer = device.ObserverFunc(func(e device.Event) { events = append(events, e) })
	c, err := Dial(ctx, s.Endpoint, o)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Browse(ctx, "ns=2;s=Missing"); err == nil {
		t.Fatal("missing node browse succeeded")
	}
	if len(events) < 2 || events[len(events)-1].Dir != device.DirRX || events[len(events)-1].Status != device.StatusException {
		t.Fatalf("missing failed response record: %+v", events)
	}
}

func testServer(t *testing.T, prepare func(*server.Server, *server.NodeNameSpace), opts ...server.Option) (*server.Server, *server.NodeNameSpace, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	opts = append(opts, server.EndPoint("127.0.0.1", port), server.EnableAuthMode(ua.UserTokenTypeAnonymous))
	s := server.New(opts...)
	ns := server.NewNodeNameSpace(s, "urn:studio:test")
	root, _ := s.Namespace(0)
	root.Objects().AddRef(ns.Objects(), id.Organizes, true)
	if prepare != nil {
		prepare(s, ns)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err = s.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); s.Close() })
	return s, ns, "opc.tcp://127.0.0.1:" + strconv.Itoa(port)
}

func TestBrowseFollowsAllContinuationPages(t *testing.T) {
	_, ns, target := testServer(t, nil, server.EnableSecurity("None", ua.MessageSecurityModeNone))
	for i := 0; i < 235; i++ {
		n := ns.AddNode(server.NewVariableNode(ua.NewStringNodeID(ns.ID(), fmt.Sprint("N", i)), fmt.Sprint("N", i), int32(i)))
		ns.Objects().AddRef(n, id.HasComponent, true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := Dial(ctx, target, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	nodes, err := c.Browse(ctx, "ns=1;i=85")
	if err != nil || len(nodes) != 235 {
		t.Fatalf("pages: %d nodes, %v", len(nodes), err)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.NodeID] {
			t.Fatal("duplicate page node", n.NodeID)
		}
		seen[n.NodeID] = true
	}
}

func TestLostWriteAcknowledgementIsUnknownAndNotRetried(t *testing.T) {
	var attempts atomic.Int32
	applied := make(chan struct{})
	var node *ua.NodeID
	_, ns, target := testServer(t, func(s *server.Server, ns *server.NodeNameSpace) {
		node = ua.NewStringNodeID(ns.ID(), "Counter")
		ns.AddNode(server.NewVariableNode(node, "Counter", int32(42)))
		s.RegisterHandler(id.WriteRequest_Encoding_DefaultBinary, func(sc *uasc.SecureChannel, r ua.Request, reqID uint32) (ua.Response, error) {
			attempts.Add(1)
			req := r.(*ua.WriteRequest)
			w := req.NodesToWrite[0]
			ns.SetAttribute(w.NodeID, w.AttributeID, w.Value)
			close(applied)
			sc.Close()
			return &ua.WriteResponse{ResponseHeader: &ua.ResponseHeader{Timestamp: time.Now(), RequestHandle: req.RequestHeader.RequestHandle, ServiceResult: ua.StatusOK, ServiceDiagnostics: &ua.DiagnosticInfo{}, AdditionalHeader: ua.NewExtensionObject(nil)}, Results: []ua.StatusCode{ua.StatusGood}}, nil
		})
	}, server.EnableSecurity("None", ua.MessageSecurityModeNone))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	o := DefaultOptions()
	o.Timeout = time.Second
	c, err := Dial(ctx, target, o)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, _ := device.ParseAddress(node.String(), "Int32", 1)
	v, _ := device.ParseValue(a, "99")
	wr, err := c.Write(ctx, a, v)
	if err == nil || wr.State != "UNKNOWN" || attempts.Load() != 1 {
		t.Fatalf("%+v err=%v attempts=%d", wr, err, attempts.Load())
	}
	<-applied
	got := ns.Attribute(node, ua.AttributeIDValue)
	if got.Value.Value() != int32(99) {
		t.Fatal("test did not apply the write")
	}
}

func certificate(t *testing.T, name string) ([]byte, *rsa.PrivateKey, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := url.Parse("urn:opcua-ai-studio:" + name)
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{uri}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cp, kp := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(cp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	return der, key, cp, kp
}

func TestEncryptedConnectionAndCertificateMismatch(t *testing.T) {
	der, key, serverCert, _ := certificate(t, "server")
	_, _, clientCert, clientKey := certificate(t, "client")
	_, ns, target := testServer(t, nil, server.EnableSecurity("Basic256Sha256", ua.MessageSecurityModeSignAndEncrypt), server.PrivateKey(key), server.Certificate(der))
	n := ns.AddNode(server.NewVariableNode(ua.NewStringNodeID(ns.ID(), "Counter"), "Counter", int32(42)))
	o := DefaultOptions()
	o.Policy = "Basic256Sha256"
	o.Mode = "SignAndEncrypt"
	o.Certificate = clientCert
	o.PrivateKey = clientKey
	o.ServerCertificate = serverCert
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := Dial(ctx, target, o)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := device.ParseAddress(n.ID().String(), "Auto", 1)
	rr, err := c.Read(ctx, []device.Address{a})
	c.Close()
	if err != nil || rr[0].Value.Integer != "42" {
		t.Fatalf("%+v %v", rr, err)
	}
	o.ServerCertificate = clientCert
	if c, err = Dial(ctx, target, o); err == nil {
		c.Close()
		t.Fatal("untrusted server accepted")
	}
}
