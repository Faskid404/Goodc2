package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password == "" {
		jsonError(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Password != dashPassword() {
		time.Sleep(500 * time.Millisecond)
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, err := issueToken("dashboard", 24*time.Hour)
	if err != nil {
		jsonError(w, "token error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"token": token})
}

func HandleListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := dbListAgents()
	if err != nil {
		jsonError(w, "db error", http.StatusInternalServerError)
		return
	}
	online := map[string]bool{}
	for _, id := range hub.online() {
		online[id] = true
	}
	for i := range rows {
		rows[i].Online = online[rows[i].ID]
	}
	if rows == nil {
		rows = []AgentRow{}
	}
	jsonOK(w, rows)
}

func HandleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	limit := 120
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, _ := strconv.Atoi(l); v > 0 && v <= 500 {
			limit = v
		}
	}
	rows, err := dbGetMetrics(agentID, limit)
	if err != nil {
		jsonError(w, "db error", http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []MetricsRow{}
	}
	jsonOK(w, rows)
}

func HandleAgentEvents(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	rows, err := dbGetEvents(agentID, 100)
	if err != nil {
		jsonError(w, "db error", http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []EventRow{}
	}
	jsonOK(w, rows)
}

func HandleAgentCommands(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		rows, err := dbGetCommands(agentID, 50)
		if err != nil {
			jsonError(w, "db error", http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []CommandRow{}
		}
		jsonOK(w, rows)

	case http.MethodPost:
		var body struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			jsonError(w, "bad request", http.StatusBadRequest)
			return
		}
		if !allowedCmd(body.Type) {
			jsonError(w, "command not permitted", http.StatusBadRequest)
			return
		}
		cmdID, err := dbCreateCommand(agentID, body.Type, body.Payload)
		if err != nil {
			jsonError(w, "db error", http.StatusInternalServerError)
			return
		}
		sent := hub.sendTo(agentID, WireMsg{
			Type: "cmd",
			ID:   cmdID,
			Payload: mustMarshal(map[string]string{
				"type": body.Type, "payload": body.Payload,
			}),
		})
		if !sent {
			dbUpdateCommand(cmdID, "failed", "agent offline")
			jsonError(w, "agent offline", http.StatusServiceUnavailable)
			return
		}
		jsonOK(w, map[string]any{"id": cmdID, "status": "sent"})

	default:
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func HandleAllEvents(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	rows, err := dbGetEvents(agentID, 200)
	if err != nil {
		jsonError(w, "db error", http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []EventRow{}
	}
	jsonOK(w, rows)
}

func HandleHealth(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, map[string]string{"status": "ok"})
}

func allowedCmd(t string) bool {
	switch t {
	case "get_metrics", "get_disk", "get_network", "get_procs", "get_sysinfo", "set_beacon", "ping":
		return true
	}
	return false
}
