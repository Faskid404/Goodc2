package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type Agent struct {
	ID       string
	Hostname string
	IP       string
	OS       string
	Arch     string
	Version  string
	LastSeen time.Time
	conn     net.Conn
	send     chan []byte
}

type WireMsg struct {
	Type    string          `json:"type"`
	ID      int64           `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Hub struct {
	mu         sync.RWMutex
	agents     map[string]*Agent
	dmu        sync.RWMutex
	dashboards map[chan []byte]struct{}
}

var hub = &Hub{
	agents:     make(map[string]*Agent),
	dashboards: make(map[chan []byte]struct{}),
}

func (h *Hub) register(a *Agent) {
	h.mu.Lock()
	h.agents[a.ID] = a
	h.mu.Unlock()
	dbUpsertAgent(a)
	dbLogEvent(a.ID, "connect", a.IP)
	log.Printf("[hub] + %s  %s  %s/%s", a.ID, a.IP, a.OS, a.Arch)
	h.broadcast(wireEvent("agent_up", map[string]string{"id": a.ID, "hostname": a.Hostname}))
}

func (h *Hub) unregister(id string) {
	h.mu.Lock()
	delete(h.agents, id)
	h.mu.Unlock()
	dbLogEvent(id, "disconnect", "")
	log.Printf("[hub] - %s", id)
	h.broadcast(wireEvent("agent_down", map[string]string{"id": id}))
}

func (h *Hub) online() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.agents))
	for id := range h.agents {
		ids = append(ids, id)
	}
	return ids
}

func (h *Hub) sendTo(agentID string, msg WireMsg) bool {
	h.mu.RLock()
	a, ok := h.agents[agentID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	data, _ := json.Marshal(msg)
	select {
	case a.send <- data:
		return true
	default:
		return false
	}
}

func (h *Hub) broadcast(data []byte) {
	h.dmu.RLock()
	defer h.dmu.RUnlock()
	for ch := range h.dashboards {
		select {
		case ch <- data:
		default:
		}
	}
}

func (h *Hub) addDash(ch chan []byte) {
	h.dmu.Lock()
	h.dashboards[ch] = struct{}{}
	h.dmu.Unlock()
}

func (h *Hub) removeDash(ch chan []byte) {
	h.dmu.Lock()
	delete(h.dashboards, ch)
	h.dmu.Unlock()
}

func wireEvent(kind string, payload any) []byte {
	p, _ := json.Marshal(payload)
	b, _ := json.Marshal(WireMsg{Type: kind, Payload: p})
	return b
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func HandleAgentWS(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		log.Printf("[ws-agent] upgrade err: %v", err)
		return
	}

	conn.SetDeadline(time.Now().Add(15 * time.Second))
	raw, err := wsutil.ReadClientText(conn)
	if err != nil {
		conn.Close()
		return
	}
	conn.SetDeadline(time.Time{})

	var hello struct {
		Hostname string `json:"hostname"`
		IP       string `json:"ip"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Hostname == "" {
		conn.Close()
		return
	}

	a := &Agent{
		ID:       "agent-" + hello.Hostname,
		Hostname: hello.Hostname,
		IP:       hello.IP,
		OS:       hello.OS,
		Arch:     hello.Arch,
		Version:  hello.Version,
		LastSeen: time.Now(),
		conn:     conn,
		send:     make(chan []byte, 128),
	}

	hub.register(a)
	defer func() { hub.unregister(a.ID); conn.Close() }()

	go a.writePump()
	a.readPump()
}

func (a *Agent) readPump() {
	for {
		a.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		raw, err := wsutil.ReadClientText(a.conn)
		if err != nil {
			return
		}
		var msg WireMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		a.LastSeen = time.Now()
		dbUpdateLastSeen(a.ID)

		switch msg.Type {
		case "metrics":
			var m MetricsRow
			if json.Unmarshal(msg.Payload, &m) == nil {
				m.AgentID = a.ID
				dbInsertMetrics(m)
				hub.broadcast(wireEvent("metrics", m))
			}
		case "cmd_result":
			var res struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
				Result string `json:"result"`
			}
			if json.Unmarshal(msg.Payload, &res) == nil {
				dbUpdateCommand(res.ID, res.Status, res.Result)
				hub.broadcast(wireEvent("cmd_result", res))
			}
		case "ping":
			pong, _ := json.Marshal(WireMsg{Type: "pong"})
			a.send <- pong
		}
	}
}

func (a *Agent) writePump() {
	ticker := time.NewTicker(25 * time.Second)
	defer func() { ticker.Stop(); a.conn.Close() }()
	for {
		select {
		case data, ok := <-a.send:
			if !ok {
				return
			}
			a.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if wsutil.WriteServerText(a.conn, data) != nil {
				return
			}
		case <-ticker.C:
			ping, _ := json.Marshal(WireMsg{Type: "ping"})
			a.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if wsutil.WriteServerText(a.conn, ping) != nil {
				return
			}
		}
	}
}

func HandleDashboardWS(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := make(chan []byte, 256)
	hub.addDash(ch)
	defer hub.removeDash(ch)

	init_, _ := json.Marshal(WireMsg{
		Type:    "init",
		Payload: mustMarshal(map[string]any{"online": hub.online()}),
	})
	wsutil.WriteServerText(conn, init_)

	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, err := wsutil.ReadClientText(conn); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-gone:
			return
		case data := <-ch:
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if wsutil.WriteServerText(conn, data) != nil {
				return
			}
		}
	}
}
