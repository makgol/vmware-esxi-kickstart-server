package tftp

import (
	"context"
	"fmt"
	"io"
	"kickstart/config"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pin/tftp/v3"
	"go.uber.org/zap"
)

type Server struct {
	logger          *zap.Logger
	fileRootDirInfo *config.FileRootDirInfo
}

func (s *Server) getReadHandler() func(string, io.ReaderFrom) error {
	return func(filename string, rf io.ReaderFrom) error {
		fullPath := filepath.Join(s.fileRootDirInfo.BootFileDirPath, filename)
		file, err := os.Open(fullPath)
		if err != nil {
			s.logger.Error("failed to open boot file", zap.Error(err))
			return err
		}
		defer file.Close()

		_, err = rf.ReadFrom(file)
		if err != nil {
			if !strings.Contains(err.Error(), "User aborted the transfer") {
				s.logger.Error("failed to send file", zap.Error(err))
				return err
			}
		}
		return nil
	}
}

func RunServer(ctx context.Context, config *config.Config, logger *zap.Logger, fileRootDirInfo *config.FileRootDirInfo) {
	srv := Server{
		logger:          logger,
		fileRootDirInfo: fileRootDirInfo,
	}
	s := tftp.NewServer(srv.getReadHandler(), nil)
	s.SetTimeout(5 * time.Second)
	logger.Info("starting TFTP server...")
	err := s.ListenAndServe(fmt.Sprintf("%s:69", config.ServicePortAddr))
	if err != nil {
		logger.Fatal("could not start TFTP Server", zap.Error(err))
		os.Exit(1)
	}
	select {
	case <-ctx.Done():
		logger.Info("tftp server: shutting down...")
		return
	default:
	}
}
