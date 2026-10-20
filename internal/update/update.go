// Package update finds a newer release on Gitee or GitHub, downloads and
// verifies the Windows package, and swaps it into the running installation.
package update

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Source is a release API returning the latest release in the Gitee or GitHub
// shape: tag_name, body, html_url and assets with browser_download_url.
type Source struct {
	Name, API, Page string
}

// Sources are tried in order; Gitee is reachable from more sites in China.
var Sources = []Source{
	{"Gitee", "https://gitee.com/api/v5/repos/xiaolengwangwang/opcua_ai_studio/releases/latest", "https://gitee.com/xiaolengwangwang/opcua_ai_studio/releases/tag/"},
	{"GitHub", "https://api.github.com/repos/xiaolengWangWang/opcua-ai-studio/releases/latest", "https://github.com/xiaolengWangWang/opcua-ai-studio/releases/tag/"},
}

// Files are the package entries an update replaces; nothing else in the
// archive is extracted.
var Files = []string{"OPCUAAIStudio.exe", "opcua-cli.exe", "opcua-sim.exe", "README.txt", "VERSION.txt"}

type Release struct {
	Version, Notes, Page, Source string
	Package, Sums, PackageName   string // download URLs and the package file name
}

func get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OPCUAAIStudio-update")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("%s 返回 %s", url, res.Status)
	}
	return res, nil
}

// Latest returns the newest release of the first source that answers with a
// Windows package and its checksum file.
func Latest(ctx context.Context, client *http.Client) (Release, error) {
	var errs []error
	for _, s := range Sources {
		r, err := latest(ctx, client, s)
		if err == nil {
			return r, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
	}
	return Release{}, fmt.Errorf("检查更新失败：%w", errors.Join(errs...))
}

func latest(ctx context.Context, client *http.Client, s Source) (Release, error) {
	res, err := get(ctx, client, s.API)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	var body struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, err
	}
	r := Release{Version: strings.TrimPrefix(body.Tag, "v"), Notes: body.Body, Page: s.Page + body.Tag, Source: s.Name}
	if _, err := parse(r.Version); err != nil {
		return Release{}, err
	}
	r.PackageName = fmt.Sprintf("OPCUAAIStudio-%s-Windows-x64.zip", r.Version)
	for _, a := range body.Assets {
		switch a.Name {
		case r.PackageName:
			r.Package = a.URL
		case "SHA256SUMS.txt":
			r.Sums = a.URL
		}
	}
	if r.Package == "" || r.Sums == "" {
		return Release{}, fmt.Errorf("v%s 缺少 %s 或 SHA256SUMS.txt", r.Version, r.PackageName)
	}
	return r, nil
}

func parse(version string) ([3]int, error) {
	var v [3]int
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("版本号 %q 须为 major.minor.patch", version)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, fmt.Errorf("版本号 %q 须为 major.minor.patch", version)
		}
		v[i] = n
	}
	return v, nil
}

// Newer reports whether version is later than current; an unparsable current
// version, such as a development build, never offers an update.
func Newer(version, current string) bool {
	a, err := parse(version)
	b, err2 := parse(current)
	if err != nil || err2 != nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Download saves the package into dir after checking it against the release's
// SHA256SUMS.txt; done reports bytes received and the total when known.
func Download(ctx context.Context, client *http.Client, r Release, dir string, done func(received, total int64)) (string, error) {
	res, err := get(ctx, client, r.Sums)
	if err != nil {
		return "", err
	}
	want := ""
	lines := bufio.NewScanner(io.LimitReader(res.Body, 64<<10))
	for lines.Scan() {
		if f := strings.Fields(lines.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == r.PackageName {
			want = strings.ToLower(f[0])
		}
	}
	res.Body.Close()
	if want == "" {
		return "", fmt.Errorf("SHA256SUMS.txt 中没有 %s", r.PackageName)
	}
	res, err = get(ctx, client, r.Package)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	path := filepath.Join(dir, r.PackageName)
	out, err := os.Create(path + ".part")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	counter := &progress{done: done, total: res.ContentLength}
	_, err = io.Copy(io.MultiWriter(out, hash, counter), res.Body)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && hex.EncodeToString(hash.Sum(nil)) != want {
		err = errors.New("下载文件的 SHA256 与发布的校验值不一致")
	}
	if err == nil {
		err = os.Rename(path+".part", path)
	}
	if err != nil {
		os.Remove(path + ".part")
		return "", err
	}
	return path, nil
}

type progress struct {
	done            func(int64, int64)
	received, total int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.received += int64(len(b))
	if p.done != nil {
		p.done(p.received, p.total)
	}
	return len(b), nil
}

// Install replaces the package files next to exe with those in the archive.
// A running exe can be renamed but not overwritten on Windows, so current
// files move aside to .old first; any failure restores them.
func Install(archive, exe string) (err error) {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	dir := filepath.Dir(exe)
	entries := map[string]*zip.File{}
	for _, f := range z.File {
		entries[filepath.Base(filepath.FromSlash(f.Name))] = f
	}
	var staged []string
	defer func() {
		for _, name := range staged {
			os.Remove(filepath.Join(dir, name+".new"))
		}
	}()
	for _, name := range Files {
		f := entries[name]
		if f == nil {
			return fmt.Errorf("更新包缺少 %s", name)
		}
		if err := extract(f, filepath.Join(dir, name+".new")); err != nil {
			return fmt.Errorf("无法写入安装目录 %s，请以有写权限的账号运行或手动更新：%w", dir, err)
		}
		staged = append(staged, name)
	}
	var moved []string
	defer func() {
		if err == nil {
			return
		}
		for _, name := range moved {
			target := filepath.Join(dir, name)
			os.Remove(target)
			os.Rename(target+".old", target)
		}
	}()
	for _, name := range Files {
		target := filepath.Join(dir, name)
		// Another window may still run the previous copy, which cannot be
		// removed but can be moved aside; Cleanup removes it once it is free.
		if err := os.Remove(target + ".old"); err != nil && !os.IsNotExist(err) {
			os.Rename(target+".old", fmt.Sprintf("%s.old-%d", target, time.Now().UnixNano()))
		}
		if _, statErr := os.Stat(target); statErr == nil {
			if err := os.Rename(target, target+".old"); err != nil {
				return err
			}
		}
		moved = append(moved, name)
		if err := os.Rename(target+".new", target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, path string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Cleanup removes the files an earlier update moved aside.
func Cleanup(exe string) {
	dir := filepath.Dir(exe)
	for _, name := range Files {
		// Copies still run by other windows stay until a later start.
		old, _ := filepath.Glob(filepath.Join(dir, name+".old*"))
		for _, path := range old {
			os.Remove(path)
		}
	}
}
