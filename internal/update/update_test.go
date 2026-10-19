package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func pack(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := z.Create("OPCUAAIStudio-9.9.9-Windows-x64/" + name)
		w.Write([]byte(body))
	}
	z.Close()
	return buf.Bytes()
}

func release(t *testing.T, archive []byte, sum string) (*httptest.Server, Release) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/down", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 503) })
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","body":"说明","assets":[{"name":"OPCUAAIStudio-9.9.9-Windows-x64.zip","browser_download_url":"%[1]s/zip"},{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/sums"}]}`, srv.URL)
	})
	mux.HandleFunc("/zip", func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  OPCUAAIStudio-9.9.9-Windows-x64.zip\n", sum)
	})
	old := Sources
	Sources = []Source{{"Gitee", srv.URL + "/down", "p/"}, {"GitHub", srv.URL + "/latest", "p/"}}
	t.Cleanup(func() { Sources = old })
	r, err := Latest(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return srv, r
}

func TestLatestFallsBackAndComparesVersions(t *testing.T) {
	_, r := release(t, nil, "")
	if r.Version != "9.9.9" || r.Source != "GitHub" || r.Notes != "说明" || r.Page != "p/v9.9.9" {
		t.Fatalf("release: %+v", r)
	}
	for _, tc := range []struct {
		version, current string
		newer            bool
	}{{"0.4.0", "0.3.0", true}, {"0.10.0", "0.9.9", true}, {"0.3.0", "0.3.0", false}, {"0.2.9", "0.3.0", false}, {"1.0.0", "dev", false}} {
		if Newer(tc.version, tc.current) != tc.newer {
			t.Fatalf("Newer(%s, %s)", tc.version, tc.current)
		}
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	archive := pack(t, map[string]string{"OPCUAAIStudio.exe": "new"})
	srv, r := release(t, archive, hex.EncodeToString(make([]byte, 32)))
	dir := t.TempDir()
	if _, err := Download(context.Background(), srv.Client(), r, dir, nil); err == nil {
		t.Fatal("tampered package accepted")
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("partial download left behind: %v", left)
	}
}

func TestInstallReplacesFilesAndRollsBack(t *testing.T) {
	files := map[string]string{}
	for _, name := range Files {
		files[name] = "new " + name
	}
	files["../evil.exe"] = "outside"
	archive := pack(t, files)
	sum := sha256.Sum256(archive)
	srv, r := release(t, archive, hex.EncodeToString(sum[:]))
	dir := t.TempDir()
	received := int64(0)
	path, err := Download(context.Background(), srv.Client(), r, dir, func(n, _ int64) { received = n })
	if err != nil || received != int64(len(archive)) {
		t.Fatalf("download: %v, %d bytes", err, received)
	}
	install := filepath.Join(dir, "app")
	os.Mkdir(install, 0o755)
	exe := filepath.Join(install, "OPCUAAIStudio.exe")
	os.WriteFile(exe, []byte("old"), 0o644)
	if err := Install(path, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new OPCUAAIStudio.exe" {
		t.Fatalf("exe not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Fatal("running exe not kept aside")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.exe")); err == nil {
		t.Fatal("entry outside the package list extracted")
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Fatal("old exe not cleaned up")
	}
	broken := filepath.Join(dir, "broken.zip")
	os.WriteFile(broken, pack(t, map[string]string{"OPCUAAIStudio.exe": "half"}), 0o644)
	if err := Install(broken, exe); err == nil {
		t.Fatal("incomplete package installed")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new OPCUAAIStudio.exe" {
		t.Fatalf("failed install changed the exe: %q", b)
	}
}
