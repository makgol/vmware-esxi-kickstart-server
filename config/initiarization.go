package config

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

func LoadServerConfig() (*Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return nil, fmt.Errorf("could not load environment variables: %w", err)
	}
	var apiPort, servicePort *PortInfo

	if cfg.APIIpAddr != "" && cfg.ServiceIpAddr != "" {
		apiPort, err = validateIP(cfg.APIIpAddr)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
		servicePort, err = validateIP(cfg.ServiceIpAddr)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
	} else if cfg.APIIpAddr != "" {
		apiPort, err = validateIP(cfg.APIIpAddr)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
		_, servicePort, err = defineInterface(false)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
	} else if cfg.ServiceIpAddr != "" {
		servicePort, err = validateIP(cfg.ServiceIpAddr)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
		apiPort, _, err = defineInterface(false)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
	} else {
		apiPort, servicePort, err = defineInterface(true)
		if err != nil {
			return nil, fmt.Errorf("error: %w", err)
		}
	}
	config := LoadDefaultConfig(apiPort, servicePort, &cfg)
	return config, nil
}

func defineInterface(requireBoth bool) (*PortInfo, *PortInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}

	var apiPort, servicePort *PortInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		if !(strings.HasPrefix(iface.Name, "eth") || strings.HasPrefix(iface.Name, "ens")) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return nil, nil, err
		}

		for _, addr := range addrs {
			ip, ipNet, err := net.ParseCIDR(addr.String())
			if err != nil {
				return nil, nil, err
			}

			ipv4 := ip.To4()
			if ipv4 == nil {
				continue
			}

			portInfo := &PortInfo{
				InterfaceName: iface.Name,
				IPAddress:     ipv4,
				SubnetMask:    ipNet.Mask,
			}

			// The first interface is defined as a port providing the API and the subsequent as a ServicePort.
			if apiPort == nil {
				apiPort = portInfo
			} else if servicePort == nil && apiPort.InterfaceName != iface.Name {
				servicePort = portInfo
				break
			}
		}
		if apiPort != nil && servicePort != nil {
			break
		}
	}

	if requireBoth && (apiPort == nil || servicePort == nil) {
		return nil, nil, errors.New("could not find two suitable network interfaces")
	}

	return apiPort, servicePort, nil
}

func validateIP(ipAddress string) (*PortInfo, error) {
	ipToFind := net.ParseIP(ipAddress)
	if ipToFind == nil {
		return nil, errors.New("invalid IP address")
	}
	ipToFind = ipToFind.To4()
	if ipToFind == nil {
		return nil, errors.New("there is not an IPv4 address")
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, i := range interfaces {
		addrs, err := i.Addrs()
		if err != nil {
			return nil, err
		}

		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				ip := v.IP.To4()
				if ip == nil {
					continue
				}
				if ip.Equal(ipToFind) {
					fmt.Printf("Found IP %v on interface %v\n", ip, i.Name)
					return &PortInfo{InterfaceName: i.Name, IPAddress: ip, SubnetMask: v.Mask}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("there are no any interfaces found with IP %v", ipToFind)
}

func getIPRange(ip net.IP, mask net.IPMask) (net.IP, net.IP) {
	startIP := ip.Mask(mask)
	endIP := make(net.IP, len(startIP))
	copy(endIP, startIP)
	for i := 0; i < len(mask); i++ {
		endIP[i] |= ^mask[i]
	}
	return startIP, endIP
}

func incrementIP(ip net.IP) net.IP {
	ip = ip.To4()
	ipInt := binary.BigEndian.Uint32(ip)
	ipInt++
	binary.BigEndian.PutUint32(ip, ipInt)
	return ip
}

func decrementIP(ip net.IP) net.IP {
	ip = ip.To4()
	ipInt := binary.BigEndian.Uint32(ip)
	ipInt--
	binary.BigEndian.PutUint32(ip, ipInt)
	return ip
}

func GetDHCPLeaseConfig(cfg *Config) *DHCPLeaseConfig {
	var startIP, endIP net.IP
	if cfg.DHCPStartIP != "" && cfg.DHCPEndIP != "" {
		startIP = net.ParseIP(cfg.DHCPStartIP).To4()
		endIP = net.ParseIP(cfg.DHCPEndIP).To4()
	} else {
		startIP, endIP = getIPRange(cfg.ServicePortAddr, cfg.ServicePortMask)
		startIP = incrementIP(startIP)
		endIP = decrementIP(endIP)
	}
	return LoadDHCPLeaseConfig(cfg, startIP, endIP)
}
