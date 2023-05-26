package dhcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"kickstart/common"
	"kickstart/config"
	"path/filepath"
	"strconv"

	"go.uber.org/zap"
	"go.universe.tf/netboot/dhcp4"
)

func intToHexBytes(n int) []byte {
	hexString := fmt.Sprintf("%08x", n)
	byteArray := make([]byte, 0, len(hexString)/2)

	for i := 0; i < len(hexString); i += 2 {
		hexByte, _ := strconv.ParseUint(hexString[i:i+2], 16, 8)
		byteArray = append(byteArray, byte(hexByte))
	}

	return byteArray
}

func RunServer(ctx context.Context, config *config.Config, logger *zap.Logger) {
	serverIP := config.ServicePortAddr
	serverNetMask := config.ServicePortMask
	listen := fmt.Sprintf("%s:67", serverIP)
	conn, err := dhcp4.NewConn(listen)
	if err != nil {
		logger.Fatal(fmt.Sprintf("unable to listen on %s", listen), zap.Error(err))
	}
	select {
	case <- ctx.Done():
		logger.Info("dhcp server: shutting down...")
		return 
	default:
	}

	defer conn.Close()

	logger.Info("starting DHCP server...")
	for {
		req, intf, err := conn.RecvDHCP()
		if err != nil {
			logger.Error("failed to receive DHCP package", zap.Error(err))
		}

		logger.Info(fmt.Sprintf("received %s from %s", req.Type, req.HardwareAddr))

		common.MacIPMapMutex.RLock()
		ip, found := common.MacIPMap[req.HardwareAddr.String()]
		common.MacIPMapMutex.RUnlock()

		if !found {
			logger.Warn(fmt.Sprintf("no IP address found for MAC address: %s", req.HardwareAddr))
			continue
		}

		common.MacFileMapMutex.RLock()
		bootFilename, found := common.MacFileMap[req.HardwareAddr.String()]
		common.MacFileMapMutex.RUnlock()
		if !found {
			logger.Warn(fmt.Sprintf("no ISO file found for MAC address: %s", req.HardwareAddr))
			continue
		}

		resp := &dhcp4.Packet{
			TransactionID: req.TransactionID,
			HardwareAddr:  req.HardwareAddr,
			ClientAddr:    req.ClientAddr,
			YourAddr:      ip,
			ServerAddr:    serverIP,
			Options:       make(dhcp4.Options),
		}
		logger.Info(fmt.Sprintf("assigned ip is %s ", ip))
		clientArch := req.Options[93]
		if clientArch != nil {
			switch binary.BigEndian.Uint16(clientArch) {
			case 0: //bios
				bootFilename = filepath.Join(bootFilename, "pxelinux.0")
			case 6, 7, 9: //uefi
				bootFilename = filepath.Join(bootFilename, "mboot.efi")
			default:
				logger.Warn(fmt.Sprintf("unknown client system architecture for MAC address: %s", req.HardwareAddr))
				continue
			}
			resp.BootFilename = bootFilename
		} else {
			logger.Warn(fmt.Sprintf("no client system architecture found for MAC address: %s", req.HardwareAddr))
		}

		resp.Options[dhcp4.OptServerIdentifier] = serverIP
		resp.Options[dhcp4.OptSubnetMask] = serverNetMask
		resp.Options[dhcp4.OptLeaseTime] = intToHexBytes(7200)

		switch req.Type {
		case dhcp4.MsgDiscover:
			resp.Broadcast = true
			resp.Type = dhcp4.MsgOffer

		case dhcp4.MsgRequest:
			resp.Type = dhcp4.MsgAck

		case dhcp4.MsgRelease:
			deleteIP := req.ClientAddr
			for k, v := range common.MacIPMap {
				if v.Equal(deleteIP) {
					common.MacIPMapMutex.Lock()
					delete(common.MacIPMap, k)
					common.MacIPMapMutex.Unlock()
					logger.Info(fmt.Sprintf("IP %s has been released and removed from macIPMap", deleteIP.String()))
					break
				}
			}
			continue

		default:
			logger.Warn(fmt.Sprintf("message type %s not supported", req.Type))
			continue
		}

		logger.Info(fmt.Sprintf("sending %s to %s", resp.Type, resp.HardwareAddr))
		err = conn.SendDHCP(resp, intf)
		if err != nil {
			logger.Error("unable to send DHCP packet", zap.Error(err))		}
	}
}
