package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/opcua"
)

type endpointHostWindow struct {
	fyne.Window
	clipboard fyne.Clipboard
}

func (w *endpointHostWindow) Clipboard() fyne.Clipboard { return w.clipboard }

func TestEndpointHostCanBeCopiedWithoutChangingConnection(t *testing.T) {
	d := searchDesktop(t)
	// Fyne's test window returns a fresh clipboard on each call.
	d.win = &endpointHostWindow{Window: d.win, clipboard: test.NewClipboard()}
	d.setEndpoint("opc.tcp://192.168.1.10:49320")
	ee := []opcua.Endpoint{
		{URL: "opc.tcp://admin-PC:49320/UA/Server", EndpointHost: "admin-PC", Policy: "None", Mode: "None"},
		{URL: "opc.tcp://[2001:db8::1]:4840", EndpointHost: "2001:db8::1", Policy: "None", Mode: "None"},
		{},
	}
	list := d.endpointList(ee)
	row := list.CreateItem()
	for i, e := range ee {
		list.UpdateItem(i, row)
		button := row.(*fyne.Container).Objects[1].(*widget.Button)
		if e.EndpointHost == "" {
			if !button.Disabled() {
				t.Fatal("copy offered without an endpoint-host")
			}
			continue
		}
		if !strings.Contains(endpointText(e), "endpoint-host="+e.EndpointHost) {
			t.Fatalf("endpoint-host missing from endpoint: %s", endpointText(e))
		}
		button.OnTapped()
		if got := d.win.Clipboard().Content(); got != "endpoint-host="+e.EndpointHost {
			t.Fatalf("clipboard = %q", got)
		}
	}
	if d.endpoint() != "opc.tcp://192.168.1.10:49320" {
		t.Fatalf("copy changed the connection: %s", d.endpoint())
	}
	info := opcua.ServerInfo{Endpoint: d.endpoint(), EndpointHost: "admin-PC"}
	if text := serverInfoText(info); !strings.Contains(text, "endpoint-host=admin-PC") {
		t.Fatalf("endpoint-host missing from server info: %s", text)
	}
}
