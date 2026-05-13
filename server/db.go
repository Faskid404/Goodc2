package main

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB(path string) {
	var err error
	db, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	db.SetMaxOpenConns(1)
	mustExec(`PRAGMA journal_mode=WAL`)
	mustExec(`CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY, hostname TEXT NOT NULL, ip TEXT NOT NULL,
		os TEXT NOT NULL, arch TEXT NOT NULL, version TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '', first_seen TEXT NOT NULL, last_seen TEXT NOT NULL
	)`)
	mustExec(`CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT, agent_id TEXT NOT NULL,
		cpu_pct REAL NOT NULL DEFAULT 0, mem_total INTEGER NOT NULL DEFAULT 0,
		mem_used INTEGER NOT NULL DEFAULT 0, disk_total INTEGER NOT NULL DEFAULT 0,
		disk_used INTEGER NOT NULL DEFAULT 0, net_rx INTEGER NOT NULL DEFAULT 0,
		net_tx INTEGER NOT NULL DEFAULT 0, proc_count INTEGER NOT NULL DEFAULT 0,
		recorded_at TEXT NOT NULL
	)`)
	mustExec(`CREATE INDEX IF NOT EXISTS idx_metrics_agent ON metrics(agent_id, recorded_at DESC)`)
	mustExec(`CREATE TABLE IF NOT EXISTS commands (
		id INTEGER PRIMARY KEY AUTOINCREMENT, agent_id TEXT NOT NULL,
		type TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending',
		result TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	mustExec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, agent_id TEXT NOT NULL,
		kind TEXT NOT NULL, message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
	)`)
	mustExec(`CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent_id, created_at DESC)`)
}

func mustExec(q string) {
	if _, err := db.Exec(q); err != nil {
		log.Fatalf("db exec: %v", err)
	}
}

type AgentRow struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
	Tags      string `json:"tags"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Online    bool   `json:"online"`
}

type MetricsRow struct {
	AgentID    string  `json:"agent_id"`
	CPUPct     float64 `json:"cpu_pct"`
	MemTotal   uint64  `json:"mem_total"`
	MemUsed    uint64  `json:"mem_used"`
	DiskTotal  uint64  `json:"disk_total"`
	DiskUsed   uint64  `json:"disk_used"`
	NetRx      uint64  `json:"net_rx"`
	NetTx      uint64  `json:"net_tx"`
	ProcCount  int     `json:"proc_count"`
	RecordedAt string  `json:"recorded_at"`
}

type CommandRow struct {
	ID        int64  `json:"id"`
	AgentID   string `json:"agent_id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type EventRow struct {
	AgentID   string `json:"agent_id"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func dbUpsertAgent(a *Agent) {
	n := now()
	db.Exec(`INSERT INTO agents (id,hostname,ip,os,arch,version,tags,first_seen,last_seen)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			hostname=excluded.hostname,ip=excluded.ip,os=excluded.os,
			arch=excluded.arch,version=excluded.version,last_seen=excluded.last_seen`,
		a.ID, a.Hostname, a.IP, a.OS, a.Arch, a.Version, "", n, n)
}

func dbUpdateLastSeen(id string) {
	db.Exec(`UPDATE agents SET last_seen=? WHERE id=?`, now(), id)
}

func dbInsertMetrics(m MetricsRow) {
	db.Exec(`INSERT INTO metrics (agent_id,cpu_pct,mem_total,mem_used,disk_total,disk_used,net_rx,net_tx,proc_count,recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		m.AgentID, m.CPUPct, m.MemTotal, m.MemUsed,
		m.DiskTotal, m.DiskUsed, m.NetRx, m.NetTx, m.ProcCount, now())
}

func dbGetMetrics(agentID string, limit int) ([]MetricsRow, error) {
	rows, err := db.Query(`SELECT agent_id,cpu_pct,mem_total,mem_used,disk_total,disk_used,
		net_rx,net_tx,proc_count,recorded_at FROM metrics WHERE agent_id=?
		ORDER BY recorded_at DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricsRow
	for rows.Next() {
		var r MetricsRow
		rows.Scan(&r.AgentID, &r.CPUPct, &r.MemTotal, &r.MemUsed,
			&r.DiskTotal, &r.DiskUsed, &r.NetRx, &r.NetTx, &r.ProcCount, &r.RecordedAt)
		out = append(out, r)
	}
	return out, nil
}

func dbListAgents() ([]AgentRow, error) {
	rows, err := db.Query(`SELECT id,hostname,ip,os,arch,version,tags,first_seen,last_seen
		FROM agents ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRow
	for rows.Next() {
		var r AgentRow
		rows.Scan(&r.ID, &r.Hostname, &r.IP, &r.OS, &r.Arch, &r.Version, &r.Tags, &r.FirstSeen, &r.LastSeen)
		out = append(out, r)
	}
	return out, nil
}

func dbCreateCommand(agentID, cmdType, payload string) (int64, error) {
	n := now()
	res, err := db.Exec(`INSERT INTO commands (agent_id,type,payload,status,created_at,updated_at)
		VALUES (?,?,?,'pending',?,?)`, agentID, cmdType, payload, n, n)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func dbUpdateCommand(id int64, status, result string) {
	db.Exec(`UPDATE commands SET status=?,result=?,updated_at=? WHERE id=?`, status, result, now(), id)
}

func dbGetCommands(agentID string, limit int) ([]CommandRow, error) {
	rows, err := db.Query(`SELECT id,agent_id,type,payload,status,result,created_at,updated_at
		FROM commands WHERE agent_id=? ORDER BY created_at DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommandRow
	for rows.Next() {
		var r CommandRow
		rows.Scan(&r.ID, &r.AgentID, &r.Type, &r.Payload, &r.Status, &r.Result, &r.CreatedAt, &r.UpdatedAt)
		out = append(out, r)
	}
	return out, nil
}

func dbLogEvent(agentID, kind, message string) {
	db.Exec(`INSERT INTO events (agent_id,kind,message,created_at) VALUES (?,?,?,?)`,
		agentID, kind, message, now())
}

func dbGetEvents(agentID string, limit int) ([]EventRow, error) {
	q := `SELECT agent_id,kind,message,created_at FROM events`
	var args []any
	if agentID != "" {
		q += ` WHERE agent_id=?`
		args = append(args, agentID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var r EventRow
		rows.Scan(&r.AgentID, &r.Kind, &r.Message, &r.CreatedAt)
		out = append(out, r)
	}
	return out, nil
}
