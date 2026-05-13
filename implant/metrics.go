package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Metrics struct {
	CPUPct    float64    `json:"cpu_pct"`
	MemTotal  uint64     `json:"mem_total"`
	MemUsed   uint64     `json:"mem_used"`
	DiskTotal uint64     `json:"disk_total"`
	DiskUsed  uint64     `json:"disk_used"`
	NetRx     uint64     `json:"net_rx"`
	NetTx     uint64     `json:"net_tx"`
	ProcCount int        `json:"proc_count"`
}

type DiskStat struct {
	Mount string `json:"mount"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

type NetIface struct {
	Name string `json:"name"`
	Rx   uint64 `json:"rx"`
	Tx   uint64 `json:"tx"`
}

type cpuSample struct{ total, idle uint64 }

var (
	prevCPU     cpuSample
	prevCPUTime time.Time
)

func collectMetrics() Metrics {
	m := Metrics{}
	m.CPUPct = cpuPercent()
	m.MemTotal, m.MemUsed = memInfo()
	m.DiskTotal, m.DiskUsed, _ = diskInfo()
	m.NetRx, m.NetTx, _ = netInfo()
	m.ProcCount = procCount()
	return m
}

func cpuPercent() float64 {
	s, err := readCPU()
	if err != nil {
		return 0
	}
	if prevCPUTime.IsZero() {
		prevCPU, prevCPUTime = s, time.Now()
		time.Sleep(300 * time.Millisecond)
		s, _ = readCPU()
	}
	dt := float64(s.total - prevCPU.total)
	di := float64(s.idle - prevCPU.idle)
	prevCPU, prevCPUTime = s, time.Now()
	if dt == 0 {
		return 0
	}
	return (1 - di/dt) * 100
}

func readCPU() (cpuSample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var total, idle uint64
		for i, fv := range fields {
			v, _ := strconv.ParseUint(fv, 10, 64)
			total += v
			if i == 3 {
				idle = v
			}
		}
		return cpuSample{total, idle}, nil
	}
	return cpuSample{}, nil
}

func memInfo() (total, used uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(parts[1], 10, 64)
		vals[strings.TrimSuffix(parts[0], ":")] = v * 1024
	}
	total = vals["MemTotal"]
	avail := vals["MemAvailable"]
	if avail == 0 {
		avail = vals["MemFree"]
	}
	if total > avail {
		used = total - avail
	}
	return
}

func diskInfo() (totalAll, usedAll uint64, stats []DiskStat) {
	for _, mount := range []string{"/", "/home", "/var"} {
		var st syscall.Statfs_t
		if syscall.Statfs(mount, &st) != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		free := st.Bfree * uint64(st.Bsize)
		used := total - free
		totalAll += total
		usedAll += used
		stats = append(stats, DiskStat{Mount: mount, Total: total, Used: used, Free: free})
	}
	return
}

func netInfo() (rxAll, txAll uint64, ifaces []NetIface) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan(); sc.Scan()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		ci := strings.Index(line, ":")
		if ci < 0 {
			continue
		}
		name := strings.TrimSpace(line[:ci])
		fields := strings.Fields(line[ci+1:])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		rxAll += rx
		txAll += tx
		ifaces = append(ifaces, NetIface{name, rx, tx})
	}
	return
}

func procCount() int {
	entries, _ := os.ReadDir("/proc")
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				n++
			}
		}
	}
	return n
}

func sysInfo() map[string]string {
	hostname, _ := os.Hostname()
	kernel, _ := os.ReadFile("/proc/version")
	loadavg, _ := os.ReadFile("/proc/loadavg")
	uptime, _ := os.ReadFile("/proc/uptime")
	return map[string]string{
		"hostname": hostname,
		"kernel":   strings.TrimSpace(string(kernel)),
		"loadavg":  strings.TrimSpace(string(loadavg)),
		"uptime":   strings.TrimSpace(string(uptime)),
	}
}
