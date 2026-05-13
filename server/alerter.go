package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

const offlineThreshold = 90 * time.Second
const alertInterval = 30 * time.Second

// track which agents we've already alerted about to avoid spam
var alerted = map[string]bool{}

func startAlerter() {
	go func() {
		ticker := time.NewTicker(alertInterval)
		defer ticker.Stop()
		for range ticker.C {
			checkOffline()
		}
	}()
	log.Println("[alerter] started — threshold:", offlineThreshold)
}

func checkOffline() {
	rows, err := dbListAgents()
	if err != nil {
		return
	}
	onlineIDs := map[string]bool{}
	for _, id := range hub.online() {
		onlineIDs[id] = true
	}

	for _, a := range rows {
		lastSeen, err := time.Parse(time.RFC3339, a.LastSeen)
		if err != nil {
			continue
		}
		silent := time.Since(lastSeen) > offlineThreshold
		wasAlerted := alerted[a.ID]

		if silent && !onlineIDs[a.ID] && !wasAlerted {
			// agent has gone silent — fire alert
			alerted[a.ID] = true
			payload := AlertPayload{
				AgentID:  a.ID,
				Hostname: a.Hostname,
				IP:       a.IP,
				OS:       a.OS,
				LastSeen: a.LastSeen,
				Kind:     "offline",
				Message:  "Agent has not reported for over 90 seconds",
			}
			log.Printf("[alerter] OFFLINE: %s (%s)", a.Hostname, a.IP)
			dbLogEvent(a.ID, "alert_offline", "no beacon for >90s")
			hub.broadcast(wireEvent("agent_alert", payload))
			sendWebhook(payload)
		}

		if !silent && onlineIDs[a.ID] && wasAlerted {
			// agent came back — clear alert + notify
			alerted[a.ID] = false
			payload := AlertPayload{
				AgentID:  a.ID,
				Hostname: a.Hostname,
				IP:       a.IP,
				OS:       a.OS,
				LastSeen: a.LastSeen,
				Kind:     "recovered",
				Message:  "Agent is back online",
			}
			log.Printf("[alerter] RECOVERED: %s", a.Hostname)
			dbLogEvent(a.ID, "alert_recovered", "agent reconnected")
			hub.broadcast(wireEvent("agent_alert", payload))
			sendWebhook(payload)
		}
	}
}

type AlertPayload struct {
	AgentID  string `json:"agent_id"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	LastSeen string `json:"last_seen"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
}

func webhookURL() string {
	return os.Getenv("WEBHOOK_URL")
}

func sendWebhook(payload AlertPayload) {
	url := webhookURL()
	if url == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     "c2_alert",
		"kind":      payload.Kind,
		"agent_id":  payload.AgentID,
		"hostname":  payload.Hostname,
		"ip":        payload.IP,
		"os":        payload.OS,
		"last_seen": payload.LastSeen,
		"message":   payload.Message,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[webhook] build req error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoodC2-Alerter/2.0")

	// Support optional webhook auth token
	if tok := os.Getenv("WEBHOOK_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[webhook] send error: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[webhook] sent %s alert for %s → HTTP %d", payload.Kind, payload.Hostname, resp.StatusCode)
}
