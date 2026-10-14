package main

import (
	"context"
	"flag"
	"log"
	"opcua-ai-studio/internal/simulator"
	"os"
	"os/signal"
)

func main() {
	endpoint := flag.String("endpoint", "opc.tcp://127.0.0.1:4840", "本机 OPC UA 演示 Endpoint")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	s, err := simulator.Start(ctx, *endpoint)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Printf("演示已启动 %s；Counter=42 / Temperature=23.5 / Enabled=true / Message / Samples，按 Ctrl+C 退出", s.Endpoint)
	<-ctx.Done()
}
