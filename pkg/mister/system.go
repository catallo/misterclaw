package mister

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SystemInfo holds system status information.
type SystemInfo struct {
	Temp        float64 `json:"temp"`
	RAMMb       int     `json:"ram_mb"`
	RAMFreeMb   int     `json:"ram_free_mb"`
	Disks       []DiskInfo `json:"disks"`
	Uptime      string  `json:"uptime"`
	IP          string  `json:"ip"`
	Hostname    string  `json:"hostname"`
}

// DiskInfo holds info about a single mounted disk.
type DiskInfo struct {
	Mount   string `json:"mount"`
	Device  string `json:"device"`
	TotalMb int    `json:"total_mb"`
	FreeMb  int    `json:"free_mb"`
	UsePct  string `json:"use_pct"`
}

// GetSystemInfo reads system information from /proc and other sources.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{}
	info.Temp = readCPUTemp()
	info.RAMMb, info.RAMFreeMb = readMemInfo()
	info.Disks = readAllDisks()
	info.Uptime = readUptime()
	info.IP = readIP()
	info.Hostname, _ = os.Hostname()
	return info
}

func readCPUTemp() float64 {
	// Try thermal zone (standard Linux path)
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	millideg, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return float64(millideg) / 1000.0
}

func readMemInfo() (totalMb, freeMb int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, _ := strconv.Atoi(fields[1])
		switch fields[0] {
		case "MemTotal:":
			totalMb = kb / 1024
		case "MemAvailable:":
			freeMb = kb / 1024
		}
	}
	return
}

func readAllDisks() []DiskInfo {
	out, err := exec.Command("df", "-m").Output()
	if err != nil {
		return nil
	}
	var disks []DiskInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := fields[5]
		// Only include /media/* mounts (SD card, USB drives)
		if !strings.HasPrefix(mount, "/media/") {
			continue
		}
		totalMb, _ := strconv.Atoi(fields[1])
		freeMb, _ := strconv.Atoi(fields[3])
		disks = append(disks, DiskInfo{
			Mount:   mount,
			Device:  fields[0],
			TotalMb: totalMb,
			FreeMb:  freeMb,
			UsePct:  fields[4],
		})
	}
	return disks
}

func readUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return "unknown"
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "unknown"
	}
	totalSecs := int(secs)
	days := totalSecs / 86400
	hours := (totalSecs % 86400) / 3600
	mins := (totalSecs % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func readIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
