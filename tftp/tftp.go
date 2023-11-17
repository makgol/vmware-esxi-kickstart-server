package tftp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"kickstart/common"
	"kickstart/config"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/pin/tftp/v3"
	"go.uber.org/zap"
)

type Server struct {
	logger          *zap.Logger
	fileRootDirInfo *config.FileRootDirInfo
	cfg             *config.Config
}

func (s *Server) getReadHandler() func(string, io.ReaderFrom) error {
	return func(filenamePath string, rf io.ReaderFrom) error {
		fmt.Println(filenamePath)
		rootBootFileDirPath := s.fileRootDirInfo.BootFileDirPath
		filename := filepath.Base(filenamePath)
		pathparts := strings.Split(filenamePath, "/")
		common.FileOSMapMutex.RLock()
		osFamily, found := common.FileOSMap[pathparts[0]]
		common.FileOSMapMutex.RUnlock()
		if found {
			if osFamily == "rhel" {
				s.logger.Info("rhel found.")
				rootBootFileDirPath = s.fileRootDirInfo.RhelBootFileDirPath
			}
		}
		var fullPath string
		var file fs.File
		var err error
		switch filename {
		case "autoexec.ipxe", "ipxe.efi", "pxelinux.0", "default", "undionly.kpxe":
			ksTemplatefiles := common.GetKsTemplatefiles()
			if osFamily == "rhel" && (filename == "autoexec.ipxe" || filename == "default") {
				fullPath = filepath.Join("templates", "rhel"+filename)
				content, err := ksTemplatefiles.ReadFile(fullPath)
				tmpl, err := template.New(filename).Parse(string(content))
				if err != nil {
				    s.logger.Error("error parsing embedded template", zap.Error(err))
				    return err
				}
				data := common.LoadBootCfgTemplateData(s.cfg.ServicePortAddr.String(), strconv.Itoa(s.cfg.APIServerPort), pathparts[0])
				var buf bytes.Buffer
				err = tmpl.Execute(&buf, data)
			    if err != nil {
				    s.logger.Error("failed to update boot file template", zap.Error(err))
				    return err
			    }
			    rf.ReadFrom(bytes.NewReader(buf.Bytes()))
			    if err != nil {
				    s.logger.Error("failed to send file", zap.Error(err))
				    return err
			    }
			    return nil
			}
			fmt.Println("test")
			fullPath = filepath.Join("templates", filename)
			file, err = ksTemplatefiles.Open(fullPath)
			if err != nil {
				s.logger.Error("failed to open boot file", zap.Error(err))
				return err
			}
		case "mboot.efi":
			common.MbootMutex.RLock()
			defer common.MbootMutex.RUnlock()
			fullPath = filepath.Join(rootBootFileDirPath, filename)
			file, err = os.Open(fullPath)
			if err != nil {
				s.logger.Error("failed to open boot file", zap.Error(err))
				return err
			}
		case "boot.cfg":
			fullPath = filepath.Join(rootBootFileDirPath, filenamePath)
			esxi6xPattern := fmt.Sprintf(`^%s/[0-9A-Fa-f]{2}-(([0-9A-Fa-f]{2}-){5}[0-9A-Fa-f]{2})/boot.cfg$`, rootBootFileDirPath)
			esxi6xRegexp := regexp.MustCompile(esxi6xPattern)
			esxi6xMatches := esxi6xRegexp.FindStringSubmatch(fullPath)
			dir := filepath.Dir(filenamePath)
			if len(esxi6xMatches) > 1 {
				macAddr := strings.Replace(esxi6xMatches[1], "-", ":", -1)
				common.MacFileMapMutex.RLock()
				bootFileVersion, found := common.MacFileMap[macAddr]
				common.MacFileMapMutex.RUnlock()
				if !found {
					err = fmt.Errorf("mapped file not found")
					s.logger.Error("failed to open boot file", zap.Error(err))
					return err
				}
				fullPath = fmt.Sprintf("%s/%s/boot.cfg", rootBootFileDirPath, bootFileVersion[0])
				dir = bootFileVersion[0]
			}
			tmpl, err := template.ParseFiles(fullPath)
			if err != nil {
				s.logger.Error("failed to open boot file", zap.Error(err))
				return err
			}
			data := common.LoadBootCfgTemplateData(s.cfg.ServicePortAddr.String(), strconv.Itoa(s.cfg.APIServerPort), dir)
			var buf bytes.Buffer
			err = tmpl.Execute(&buf, data)
			if err != nil {
				s.logger.Error("failed to update boot file template", zap.Error(err))
				return err
			}
			rf.ReadFrom(bytes.NewReader(buf.Bytes()))
			if err != nil {
				s.logger.Error("failed to send file", zap.Error(err))
				return err
			}
			return nil
		default:
			fullPath = filepath.Join(rootBootFileDirPath, filenamePath)
			file, err = os.Open(fullPath)
			if err != nil {
				s.logger.Error("failed to open boot file", zap.Error(err))
				return err
			}
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
		cfg:             config,
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
