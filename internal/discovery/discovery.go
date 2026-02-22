// Package discovery scans networks for Proxmox VE instances.
package discovery

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Instance represents a Proxmox instance found on the network.
type Instance struct {
	IP  string `json:"ip"`
	URL string `json:"url"` // https://<ip>:8006
}

// Result holds the outcome of a network scan.
type Result struct {
	Instances []Instance // discovered Proxmox instances
	Subnets   []string   // the subnets that were scanned
}

// Scan discovers Proxmox instances on the given subnets.
// Each subnet should be a CIDR string like "172.20.20.0/24".
// If no subnets are provided, the local machine's subnets are used.
func Scan(subnets []string) (*Result, error) {
	if len(subnets) == 0 {
		auto, err := LocalSubnets()
		if err != nil {
			return nil, fmt.Errorf("detecting local subnets: %w", err)
		}
		subnets = auto
	}
	if len(subnets) == 0 {
		return &Result{}, nil
	}

	// Collect all IPs to scan.
	var ips []string
	for _, subnet := range subnets {
		ips = append(ips, expandSubnet(subnet)...)
	}

	// TCP scan port 8006 with worker pool.
	const workers = 50
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var openIPs []string
	var wg sync.WaitGroup

	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			conn, err := net.DialTimeout("tcp", ip+":8006", 800*time.Millisecond)
			if err != nil {
				return
			}
			conn.Close()
			mu.Lock()
			openIPs = append(openIPs, ip)
			mu.Unlock()
		}(ip)
	}
	wg.Wait()

	// Verify each open port is actually Proxmox.
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	var results []Instance
	for _, ip := range openIPs {
		if inst, ok := verifyProxmox(httpClient, ip); ok {
			results = append(results, inst)
		}
	}

	return &Result{Instances: results, Subnets: subnets}, nil
}

// LocalSubnets returns the /24 CIDR ranges for all active, non-loopback IPv4 interfaces.
func LocalSubnets() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var subnets []string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			subnet := fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2])
			if !seen[subnet] {
				seen[subnet] = true
				subnets = append(subnets, subnet)
			}
		}
	}
	return subnets, nil
}

// NormalizeSubnet takes a flexible input (IP, IP/CIDR, or partial IP) and
// returns a /24 CIDR string. Examples:
//
//	"172.20.20.5"     → "172.20.20.0/24"
//	"172.20.20.0/24"  → "172.20.20.0/24"
//	"172.20.20"       → "172.20.20.0/24"
func NormalizeSubnet(s string) (string, error) {
	// Already a CIDR?
	if _, _, err := net.ParseCIDR(s); err == nil {
		ip, ipNet, _ := net.ParseCIDR(s)
		base := ip.Mask(ipNet.Mask).To4()
		if base == nil {
			return "", fmt.Errorf("not an IPv4 CIDR: %s", s)
		}
		return fmt.Sprintf("%d.%d.%d.0/24", base[0], base[1], base[2]), nil
	}

	// Plain IP?
	if ip := net.ParseIP(s); ip != nil {
		ip4 := ip.To4()
		if ip4 == nil {
			return "", fmt.Errorf("not an IPv4 address: %s", s)
		}
		return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2]), nil
	}

	// Partial like "172.20.20"?
	trial := net.ParseIP(s + ".0")
	if trial != nil {
		ip4 := trial.To4()
		if ip4 != nil {
			return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2]), nil
		}
	}

	return "", fmt.Errorf("cannot parse %q as IP or CIDR", s)
}

// expandSubnet returns all 254 usable host IPs in a /24 CIDR.
func expandSubnet(cidr string) []string {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	base := ip.Mask(ipNet.Mask).To4()
	if base == nil {
		return nil
	}
	ips := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		ips = append(ips, fmt.Sprintf("%d.%d.%d.%d", base[0], base[1], base[2], i))
	}
	return ips
}

// verifyProxmox checks if an IP running port 8006 is a Proxmox instance by
// hitting the PVE API endpoint and accepting any HTTP response.
func verifyProxmox(client *http.Client, ip string) (Instance, bool) {
	url := fmt.Sprintf("https://%s:8006", ip)

	resp, err := client.Get(url + "/api2/json/version")
	if err != nil {
		return Instance{}, false
	}
	resp.Body.Close()

	return Instance{IP: ip, URL: url}, true
}
