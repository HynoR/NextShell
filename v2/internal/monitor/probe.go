// Package monitor runs bounded Linux probes over a separate exec channel.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ProbeCommand = `export LC_ALL=C
[ "$(uname -s)" = Linux ] || { echo 'Linux remote monitoring only'; exit 44; }
printf '\n---NS_INFO---\n'; uname -a; hostname
printf '\n---NS_LOADAVG---\n'; cat /proc/loadavg
printf '\n---NS_CPUSTAT---\n'; head -n 1 /proc/stat
printf '\n---NS_MEMINFO---\n'; cat /proc/meminfo
printf '\n---NS_NETDEV---\n'; cat /proc/net/dev
printf '\n---NS_DISK---\n'; df -Pk
printf '\n---NS_PROCESSES---\n'; ps -eo pid,ppid,user,stat,ni,pri,pcpu,pmem,rss,vsz,etimes,comm --no-headers --sort=-pcpu
if command -v ss >/dev/null 2>&1; then printf '\n---NS_NETWORK_SS---\n'; ss -tunap; elif command -v netstat >/dev/null 2>&1; then printf '\n---NS_NETWORK_NETSTAT---\n'; netstat -tunap; else printf '\n---NS_NETWORK_UNAVAILABLE---\n'; echo 'ss/netstat unavailable'; fi
printf '\n---NS_PROBE_END---\n'`

// No user-controlled shell text is interpolated into ProbeCommand.
type Runner interface {
	Output(context.Context, string, int) ([]byte, error)
}
type Disk struct {
	Device, Mount                string
	TotalKB, UsedKB, AvailableKB uint64
	UsedPercent                  float64
}
type Process struct {
	PID, PPID, Nice, Priority, Elapsed      int
	User, State, Command                    string
	CPU, MemoryPercent, MemoryMB, VirtualMB float64
}
type Network struct {
	Protocol, State, LocalIP, RemoteIP, Process string
	LocalPort, RemotePort, PID                  int
	Listener                                    bool
	Connections, UniqueIPs                      int
}
type Snapshot struct {
	At                                                time.Time
	Info, Load                                        string
	CPUTotal, CPUIdle                                 uint64
	CPUPercent                                        float64
	HasDelta                                          bool
	MemoryTotal, MemoryAvailable, SwapTotal, SwapFree uint64
	RX, TX                                            uint64
	RXPerSecond, TXPerSecond                          float64
	Disks                                             []Disk
	Processes                                         []Process
	Network                                           []Network
	Warnings                                          []string
}

var ansi = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func Sections(raw string) map[string]string {
	raw = ansi.ReplaceAllString(strings.ReplaceAll(raw, "\r", ""), "")
	out := map[string]string{}
	key := ""
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "---NS_") && strings.HasSuffix(line, "---") {
			key = strings.TrimSuffix(strings.TrimPrefix(line, "---NS_"), "---")
			if key == "PROBE_END" {
				key = ""
			}
			continue
		}
		if key != "" {
			if out[key] != "" {
				out[key] += "\n"
			}
			out[key] += line
		}
	}
	return out
}
func uintn(s string) uint64 { v, _ := strconv.ParseUint(s, 10, 64); return v }
func intn(s string) int     { v, _ := strconv.Atoi(s); return v }
func floatn(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
func Parse(raw string, at time.Time, previous *Snapshot) (Snapshot, error) {
	s := Snapshot{At: at}
	v := Sections(raw)
	cpu := strings.Fields(v["CPUSTAT"])
	if len(cpu) < 5 || cpu[0] != "cpu" {
		return s, errors.New("missing/invalid CPUSTAT")
	}
	for i, n := range cpu[1:min(9, len(cpu))] {
		x, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			return s, errors.New("invalid CPU counter")
		}
		s.CPUTotal += x
		if i == 3 || i == 4 {
			s.CPUIdle += x
		}
	}
	mem := map[string]uint64{}
	for _, line := range strings.Split(v["MEMINFO"], "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			mem[strings.TrimSuffix(f[0], ":")] = uintn(f[1]) * 1024
		}
	}
	s.MemoryTotal = mem["MemTotal"]
	if s.MemoryTotal == 0 {
		return s, errors.New("missing/invalid MEMINFO")
	}
	if a, ok := mem["MemAvailable"]; ok {
		s.MemoryAvailable = a
	} else {
		s.MemoryAvailable = mem["MemFree"] + mem["Buffers"] + mem["Cached"] + mem["SReclaimable"]
		if s.MemoryAvailable >= mem["Shmem"] {
			s.MemoryAvailable -= mem["Shmem"]
		}
	}
	s.MemoryAvailable = min(s.MemoryAvailable, s.MemoryTotal)
	s.SwapTotal = mem["SwapTotal"]
	s.SwapFree = min(mem["SwapFree"], s.SwapTotal)
	s.Info = strings.TrimSpace(v["INFO"])
	s.Load = strings.TrimSpace(v["LOADAVG"])
	netdev := v["NETDEV"]
	if netdev == "" && v["NETCOUNTERS"] == "" {
		return s, errors.New("missing network counters")
	}
	if netdev != "" {
		for _, line := range strings.Split(netdev, "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "lo" {
				continue
			}
			f := strings.Fields(parts[1])
			if len(f) >= 9 {
				s.RX += uintn(f[0])
				s.TX += uintn(f[8])
			}
		}
	} else {
		f := strings.Fields(v["NETCOUNTERS"])
		if len(f) != 2 {
			return s, errors.New("invalid NETCOUNTERS")
		}
		for _, v := range f {
			if _, e := strconv.ParseUint(v, 10, 64); e != nil {
				return s, errors.New("invalid NETCOUNTERS")
			}
		}
		s.RX = uintn(f[0])
		s.TX = uintn(f[1])
	}
	for _, line := range strings.Split(v["DISK"], "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || strings.Contains(f[1], "blocks") {
			continue
		}
		if _, e := strconv.ParseUint(f[1], 10, 64); e != nil {
			continue
		}
		s.Disks = append(s.Disks, Disk{Device: f[0], Mount: strings.Join(f[5:], " "), TotalKB: uintn(f[1]), UsedKB: uintn(f[2]), AvailableKB: uintn(f[3]), UsedPercent: floatn(strings.TrimSuffix(f[4], "%"))})
	}
	s.Processes = ParseProcesses(v["PROCESSES"])
	if text, ok := v["NETWORK_SS"]; ok {
		s.Network = ParseSS(text)
	} else if text, ok := v["NETWORK_NETSTAT"]; ok {
		s.Network = ParseNetstat(text)
	} else {
		s.Warnings = append(s.Warnings, "network process data unavailable (ss/netstat missing or permission denied)")
	}
	if previous != nil {
		elapsed := at.Sub(previous.At).Seconds()
		if elapsed > 0 && s.CPUTotal > previous.CPUTotal && s.CPUIdle >= previous.CPUIdle {
			total := s.CPUTotal - previous.CPUTotal
			idle := min(total, s.CPUIdle-previous.CPUIdle)
			s.CPUPercent = 100 * float64(total-idle) / float64(total)
			s.HasDelta = true
		}
		if elapsed > 0 && s.RX >= previous.RX && s.TX >= previous.TX {
			s.RXPerSecond = float64(s.RX-previous.RX) / elapsed
			s.TXPerSecond = float64(s.TX-previous.TX) / elapsed
		}
	}
	return s, nil
}
func Collect(ctx context.Context, r Runner, previous *Snapshot) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, e := r.Output(ctx, ProbeCommand, 8<<20)
	if e != nil {
		return Snapshot{}, e
	}
	return Parse(string(b), time.Now(), previous)
}
func ParseProcesses(raw string) []Process {
	var out []Process
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 12 || intn(f[0]) <= 0 {
			continue
		}
		out = append(out, Process{PID: intn(f[0]), PPID: intn(f[1]), User: f[2], State: f[3], Nice: intn(f[4]), Priority: intn(f[5]), CPU: floatn(f[6]), MemoryPercent: floatn(f[7]), MemoryMB: floatn(f[8]) / 1024, VirtualMB: floatn(f[9]) / 1024, Elapsed: intn(f[10]), Command: strings.Join(f[11:], " ")})
	}
	return out
}

var pidPattern = regexp.MustCompile(`pid=(\d+)`)
var processPattern = regexp.MustCompile(`\("([^"]+)"`)

func address(v string) (string, int) {
	i := strings.LastIndex(v, ":")
	if i < 0 {
		return "*", 0
	}
	return strings.Trim(v[:i], "[]"), intn(v[i+1:])
}
func ParseSS(raw string) []Network {
	var out []Network
	states := map[string]bool{}
	for _, s := range []string{"LISTEN", "UNCONN", "ESTAB", "ESTABLISHED", "TIME-WAIT", "TIME_WAIT", "CLOSE-WAIT", "CLOSE_WAIT", "SYN-SENT", "SYN_SENT", "SYN-RECV", "SYN_RECV", "FIN-WAIT-1", "FIN-WAIT-2", "LAST-ACK"} {
		states[s] = true
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n") {
		f := strings.Fields(line)
		offset := 0
		proto := "tcp"
		if len(f) >= 6 && states[f[1]] {
			proto = f[0]
			offset = 1
		}
		if len(f) < 5+offset || !states[f[offset]] {
			continue
		}
		ip, port := address(f[offset+3])
		peer, peerPort := address(f[offset+4])
		if port <= 0 {
			continue
		}
		extra := strings.Join(f[offset+5:], " ")
		pid, name := 0, "unknown"
		if m := pidPattern.FindStringSubmatch(extra); len(m) > 1 {
			pid = intn(m[1])
		}
		if m := processPattern.FindStringSubmatch(extra); len(m) > 1 {
			name = m[1]
		}
		state := f[offset]
		if offset == 0 && state == "UNCONN" {
			proto = "udp"
		}
		out = append(out, Network{Protocol: proto, State: state, LocalIP: ip, LocalPort: port, RemoteIP: peer, RemotePort: peerPort, PID: pid, Process: name, Listener: state == "LISTEN" || state == "UNCONN"})
	}
	return countListeners(out)
}
func ParseNetstat(raw string) []Network {
	var out []Network
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || (!strings.HasPrefix(f[0], "tcp") && !strings.HasPrefix(f[0], "udp")) {
			continue
		}
		local, lp := address(f[3])
		remote, rp := address(f[4])
		if lp <= 0 {
			continue
		}
		state := f[5]
		pidProg := ""
		if strings.Contains(state, "/") {
			pidProg = state
			state = "UNCONN"
		} else if len(f) > 6 {
			pidProg = f[6]
		}
		pid, name := 0, "unknown"
		if p := strings.SplitN(pidProg, "/", 2); len(p) == 2 {
			pid = intn(p[0])
			name = p[1]
		}
		out = append(out, Network{Protocol: f[0], State: state, LocalIP: local, LocalPort: lp, RemoteIP: remote, RemotePort: rp, PID: pid, Process: name, Listener: state == "LISTEN" || state == "UNCONN"})
	}
	return countListeners(out)
}
func countListeners(out []Network) []Network {
	for i, n := range out {
		if !n.Listener {
			continue
		}
		ips := map[string]bool{}
		for _, c := range out {
			if !c.Listener && c.LocalPort == n.LocalPort && strings.TrimSuffix(c.Protocol, "6") == strings.TrimSuffix(n.Protocol, "6") && (n.LocalIP == "*" || n.LocalIP == "0.0.0.0" || n.LocalIP == "::" || n.LocalIP == c.LocalIP) {
				out[i].Connections++
				ips[c.RemoteIP] = true
			}
		}
		out[i].UniqueIPs = len(ips)
	}
	return out
}
func SignalCommand(pid int, signal string) (string, error) {
	if pid <= 1 {
		return "", errors.New("refusing pid <=1")
	}
	switch signal {
	case "TERM", "KILL", "STOP", "CONT":
	default:
		return "", errors.New("invalid signal")
	}
	return fmt.Sprintf("kill -s %s %d", signal, pid), nil
}
