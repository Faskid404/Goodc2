package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

var (
	serverURL      = envOr("C2_URL", "ws://localhost:8080/ws/agent")
	agentTok       = envOr("AGENT_TOKEN", "c2-agent-token-change-in-prod")
	beaconInterval = 30 * time.Second
)

type WireMsg struct {
	Type    string          `json:"type"`
	ID      int64           `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("[agent] %s/%s — connecting to %s", runtime.GOOS, runtime.GOARCH, serverURL)
	for attempt := 0; ; attempt++ {
		if err := run(); err != nil {
			log.Printf("[agent] lost: %v", err)
		}
		d := backoff(attempt)
		log.Printf("[agent] retry in %s", d.Round(time.Millisecond))
		time.Sleep(d)
	}
}

func run() error {
	dialer := ws.Dialer{
		Header: ws.HandshakeHeaderHTTP(http.Header{
			"X-Agent-Token": []string{agentTok},
		}),
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		Timeout:         15 * time.Second,
	}
	conn, _, _, err := dialer.Dial(serverURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	hostname, _ := os.Hostname()
	hello, _ := json.Marshal(map[string]string{
		"hostname": hostname,
		"ip":       outboundIP(),
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"version":  "2.0.0",
	})
	if err := wsutil.WriteClientText(conn, hello); err != nil {
		return err
	}
	log.Printf("[agent] connected as %s", hostname)

	ticker := time.NewTicker(beaconInterval)
	defer ticker.Stop()

	incoming := make(chan WireMsg, 32)
	fault := make(chan error, 1)

	go func() {
		for {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			raw, err := wsutil.ReadServerText(conn)
			if err != nil {
				fault <- err
				return
			}
			var m WireMsg
			if json.Unmarshal(raw, &m) == nil {
				incoming <- m
			}
		}
	}()

	for {
		select {
		case err := <-fault:
			return err
		case msg := <-incoming:
			handleMsg(conn, msg)
		case <-ticker.C:
			if err := sendBeacon(conn); err != nil {
				return err
			}
		}
	}
}

func handleMsg(conn net.Conn, msg WireMsg) {
	switch msg.Type {
	case "ping":
		pong, _ := json.Marshal(WireMsg{Type: "ping"})
		wsutil.WriteClientText(conn, pong)
	case "cmd":
		var cmd struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(msg.Payload, &cmd) == nil {
			go dispatchCmd(conn, msg.ID, cmd.Type, cmd.Payload)
		}
	}
}

func dispatchCmd(conn net.Conn, id int64, kind, payload string) {
	var result any
	status := "ok"

	switch kind {
	case "get_metrics":
		result = collectMetrics()
	case "get_disk":
		_, _, result = diskInfo()
	case "get_network":
		_, _, result = netInfo()
	case "get_procs":
		result = map[string]int{"count": procCount()}
	case "get_sysinfo":
		result = sysInfo()
	case "set_beacon":
		secs, err := strconv.Atoi(strings.TrimSpace(payload))
		if err != nil || secs < 5 || secs > 3600 {
			status = "error"
			result = "interval must be 5–3600 seconds"
		} else {
			beaconInterval = time.Duration(secs) * time.Second
			result = map[string]int{"interval_seconds": secs}
		}
	case "ping":
		result = map[string]string{"pong": time.Now().UTC().Format(time.RFC3339)}
	default:
		status = "error"
		result = "unknown command"
	}

	rb, _ := json.Marshal(result)
	resp, _ := json.Marshal(WireMsg{
		Type: "cmd_result",
		ID:   id,
		Payload: mustMarshal(map[string]any{
			"id": id, "status": status, "result": string(rb),
		}),
	})
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	wsutil.WriteClientText(conn, resp)
}

func sendBeacon(conn net.Conn) error {
	m := collectMetrics()
	payload, _ := json.Marshal(m)
	msg, _ := json.Marshal(WireMsg{Type: "metrics", Payload: payload})
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return wsutil.WriteClientText(conn, msg)
}

func backoff(n int) time.Duration {
	base := math.Min(float64(n+1)*2, 60)
	jitter := rand.Float64() * 4
	return time.Duration((base+jitter)*float64(time.Second))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func outboundIP() string {
	c := &http.Client{Timeout: 5 * time.Second}
	r, err := c.Get("https://api.ipify.org")
	if err != nil {
		return "unknown"
	}
	defer r.Body.Close()
	ip, _ := bufio.NewReader(r.Body).ReadString('\n')
	return strings.TrimSpace(ip)
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
