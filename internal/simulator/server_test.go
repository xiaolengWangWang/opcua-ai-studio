package simulator

import (
	"context"
	"net"
	"net/url"
	"testing"
)

func TestDemoStartsOnLoopbackAndReleasesPort(t *testing.T) {
	s, err := Start(context.Background(), "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatal("demo port leaked:", err)
	}
	l.Close()
}
