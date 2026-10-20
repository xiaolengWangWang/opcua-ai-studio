package ui

import (
	"fyne.io/fyne/v2/test"
	"opcua-ai-studio/internal/service"
	"testing"
)

func TestDesktopDefaultsAndCredentialsAreNotSaved(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	if !d.singleWriteBtn.Disabled() || !d.writeSelectedBtn.Disabled() {
		t.Fatal("writes must start disabled")
	}
	d.setEndpoint("opc.tcp://127.0.0.1:4840")
	d.password.SetText("test-password")
	d.saveInputs()
	if a.Preferences().String("password") != "" || a.Preferences().String("write-value") != "" {
		t.Fatal("private or write inputs persisted")
	}
	d.addresses.SetText("ns=2;s=Tag,Name\nns=2;s=Another")
	pp, err := d.points()
	if err != nil || len(pp) != 2 || pp[0].Address != "ns=2;s=Tag,Name" {
		t.Fatalf("%+v %v", pp, err)
	}
}
func TestDisconnectedDesktopRejectsWrites(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	d.singleNode.SetText("ns=2;s=Counter")
	d.writeSingle()
	if !d.singleWriteBtn.Disabled() || !d.lastError.Visible() || d.lastError.Text != "请先连接" {
		t.Fatal("write offered without connection")
	}
}
