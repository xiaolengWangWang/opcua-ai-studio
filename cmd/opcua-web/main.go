package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/web"
)

var version = "0.8.0"

func main() {
	port := flag.Int("port", 8787, "本机 Web 端口，0 表示自动选择空闲端口")
	noBrowser := flag.Bool("no-browser", false, "启动服务并打印地址，跳过浏览器启动")
	recordDir := flag.String("record-dir", "", "通信记录目录，默认使用用户配置目录")
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Parse()
	if flag.NArg() != 0 || *port < 0 || *port > 65535 {
		flag.Usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *port, *noBrowser, *recordDir); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, port int, noBrowser bool, recordDir string) (err error) {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("无法监听 %s，可用 -port 指定其他端口: %w", address, err)
	}
	host := listener.Addr().String()
	url := "http://" + host + "/"
	controller := service.New(service.Options{RecordDir: recordDir})
	defer func() { err = errors.Join(err, controller.Close()) }()
	server := &http.Server{
		Handler:           web.New(controller, host),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       time.Minute,
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	fmt.Printf("OPC UA AI Studio Web %s\n浏览器访问 %s\n", version, url)
	if !noBrowser && runtime.GOOS == "linux" && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		go openBrowser(url)
	}
	select {
	case err = <-served:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, server.Close())
		}
		return nil
	}
}

func openBrowser(url string) {
	if path, err := exec.LookPath("xdg-open"); err == nil {
		if exec.Command(path, url).Run() == nil {
			return
		}
	}
	if path, err := exec.LookPath("gio"); err == nil {
		if exec.Command(path, "open", url).Run() == nil {
			return
		}
	}
	log.Printf("无法自动打开默认浏览器，请访问 %s", url)
}
