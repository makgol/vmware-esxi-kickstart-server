package config

import (
	"net"
)

type Config struct {
	APIPortAddr     net.IP     `split_words:"true"`
	APIPortMask     net.IPMask `split_words:"true"`
	APIServerPort   int        `default:"80" split_words:"true"`
	ServicePortName string     `split_words:"true"`
	ServicePortAddr net.IP     `split_words:"true"`
	ServicePortMask net.IPMask `split_words:"true"`
	ServiceIpAddr   string     `split_words:"true"`
	APIIpAddr       string     `split_words:"true"`
	DHCPStartIP     string     `split_words:"true"`
	DHCPEndIP       string     `split_words:"true"`
	KsDirPath       string     `default:"./" split_words:"true"`
	FileDirPath     string     `default:"./files" split_words:"true"`
	LogFilePath     string     `default:"/var/log/ks-server.log" split_words:"true"`
}

type PortInfo struct {
	InterfaceName string
	IPAddress     net.IP
	SubnetMask    net.IPMask
}

func LoadDefaultConfig(apiPort, servicePort *PortInfo, cfg *Config) *Config {
	return &Config{
		APIPortAddr:     apiPort.IPAddress,
		APIPortMask:     apiPort.SubnetMask,
		APIServerPort:   cfg.APIServerPort,
		ServicePortName: servicePort.InterfaceName,
		ServicePortAddr: servicePort.IPAddress,
		ServicePortMask: servicePort.SubnetMask,
		KsDirPath:       cfg.KsDirPath,
		FileDirPath:     cfg.FileDirPath,
		LogFilePath:     cfg.LogFilePath,
	}
}

type DHCPLeaseConfig struct {
	DHCPInterfaceName string
	DHCPStartIP       net.IP
	DHCPEndIP         net.IP
}

func LoadDHCPLeaseConfig(config *Config, startIP, endIP net.IP) *DHCPLeaseConfig {
	return &DHCPLeaseConfig{
		DHCPInterfaceName: config.ServicePortName,
		DHCPStartIP:       startIP,
		DHCPEndIP:         endIP,
	}
}

type FileRootDirInfo struct {
	BootFileDirPath     string
	UploadedISODirPath  string
	RhelBootFileDirPath string
}

func LoadDirInfo(bootFileDir, uploadedISODir, rhelBootFileDir string) *FileRootDirInfo {
	return &FileRootDirInfo{
		BootFileDirPath:    bootFileDir,
		UploadedISODirPath: uploadedISODir,
		RhelBootFileDirPath: rhelBootFileDir,
	}
}
