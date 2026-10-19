package opcua

import (
	"context"
	"testing"
	"time"

	"opcua-ai-studio/internal/simulator"
)

func TestProbeSessionsStopsAtLimitAndClosesThem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sim, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	seen := 0
	n, err := ProbeSessions(ctx, sim.Endpoint, DefaultOptions(), 12, func(open int) { seen = open })
	if err != nil || n != 12 || seen != 12 {
		t.Fatalf("probe: %d sessions, %v", n, err)
	}
	if _, err := ProbeSessions(ctx, "opc.tcp://127.0.0.1:1", DefaultOptions(), 3, nil); err == nil {
		t.Fatal("refused first session not reported")
	}
}
