package main

import (
	"context"
	"fmt"
	"kickstart/api"
	"kickstart/common"
	"kickstart/config"
	"kickstart/dhcp"
	"kickstart/tftp"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func initializeFileRootDir(dirPath string) (*config.FileRootDirInfo, error) {
	bootFileDir := filepath.Join(dirPath, "bootfiles")
	err := os.MkdirAll(bootFileDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to create boot file directory: %w", err)
	}
	err = os.Chmod(bootFileDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to change permissions of the boot file directory: %w", err)
	}

	uploadedISODir := filepath.Join(dirPath, "isofiles")
	err = os.MkdirAll(uploadedISODir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload file directory: %w", err)
	}
	err = os.Chmod(uploadedISODir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to change permissions of the upload file directory: %w", err)
	}

	rhelBootFileDir := filepath.Join(dirPath, "rhelbootfiles")
	err = os.MkdirAll(rhelBootFileDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to create boot file directory: %w", err)
	}
	err = os.Chmod(rhelBootFileDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to change permissions of the boot file directory: %w", err)
	}

	return config.LoadDirInfo(bootFileDir, uploadedISODir, rhelBootFileDir), nil
}

func main() {
	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	config, err := config.LoadServerConfig()
	if err != nil {
		fmt.Printf("failed to loading server config: %v\n", err)
		return
	}

	logger, err := common.NewLogger(ctx, config.LogFilePath)
	if err != nil {
		fmt.Printf("failed to initialize logger: %v", err)
		return
	}

	fileRootDirInfo, err := initializeFileRootDir(config.FileDirPath)
	if err != nil {
		fmt.Printf("failed to initializing KS directory: %v\n", err)
		return
	}

	go api.RunServer(ctx, config, logger, fileRootDirInfo)
	go dhcp.RunServer(ctx, config, logger)
	go tftp.RunServer(ctx, config, logger, fileRootDirInfo)

	<-sigChannel
	cancel()
	logger.Info("shutting down main function")
}
