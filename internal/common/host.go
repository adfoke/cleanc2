package common

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
)

type HostInfo struct {
	Hostname    string
	OS          string
	Arch        string
	IPAddrs     []string
	Fingerprint string
}

func CollectHostInfo() HostInfo {
	hostname, _ := os.Hostname()
	ipAddrs := collectIPAddrs()
	sum := sha256.Sum256([]byte(hostname + "|" + strings.Join(ipAddrs, ",")))

	return HostInfo{
		Hostname:    hostname,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		IPAddrs:     ipAddrs,
		Fingerprint: hex.EncodeToString(sum[:]),
	}
}

func collectIPAddrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var addrs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range ifaceAddrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() {
				continue
			}
			if v4 := ipNet.IP.To4(); v4 != nil {
				addrs = append(addrs, v4.String())
			}
		}
	}

	sort.Strings(addrs)
	return addrs
}
