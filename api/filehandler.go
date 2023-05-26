package api

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"kickstart/common"
	"kickstart/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

func validateESXiISOFile(xmlPath string) error {
	_, err := os.Stat(xmlPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("target XML file does not exist: %s", xmlPath)
	}

	xmlFile, err := os.Open(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to open XML file: %v", err)
	}
	defer xmlFile.Close()

	xmlData, err := io.ReadAll(xmlFile)
	if err != nil {
		return fmt.Errorf("failed to read XML file: %v", err)
	}

	var vum common.Vum
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	err = decoder.Decode(&vum)
	if err != nil {
		return fmt.Errorf("failed to parse XML file: %v", err)
	}

	if vum.Product.EsxName == "" {
		return fmt.Errorf("ESX Version is not present in XML file: %s", xmlPath)
	}

	return nil
}

func (s *Server) ExtractISOfiles(config *config.Config, esxiFilePath, filename string) error {
	sourceISO := esxiFilePath
	bootFileDir := s.FileRootDirInfo.BootFileDirPath
	currentTime := time.Now().Format("20060102150405")
	tmpDirPrefix := "iso-" + currentTime + "-"
	tmpDir, err := os.MkdirTemp(s.FileRootDirInfo.UploadedISODirPath, tmpDirPrefix)
	if err != nil {
		s.logger.Error("failed to create temporary directory", zap.Error(err))
		return err
	}

	defer os.RemoveAll(tmpDir)
	err = os.Chmod(tmpDir, 0777)
	if err != nil {
		s.logger.Error("failed to change permissions of the temporary directory", zap.Error(err))
		return err
	}

	isoRead := filepath.Join(tmpDir, "iso_read")
	isoWriteRoot := filepath.Join(bootFileDir, filename)
	isoWrite := filepath.Join(isoWriteRoot, "esxi")
	biosBootDir := filepath.Join(isoWriteRoot, "pxelinux.cfg")

	err = os.MkdirAll(isoRead, 0777)
	if err != nil {
		s.logger.Error("failed to create iso read directory", zap.Error(err))
		return err
	}

	err = os.Chmod(isoRead, 0777)
	if err != nil {
		s.logger.Error("failed to change premissions iso read directory", zap.Error(err))
		return err
	}

	err = exec.Command("mount", "-o", "loop", sourceISO, isoRead).Run()
	if err != nil {
		s.logger.Error("failed to mount iso file", zap.Error(err))
		return err
	}

	xmlPath := filepath.Join(isoRead, "upgrade", "metadata.xml")
	err = validateESXiISOFile(xmlPath)
	if err != nil {
		s.logger.Error("failed to validate ESXi iso file", zap.Error(err))
		exec.Command("umount", isoRead).Run()
		os.RemoveAll(isoRead)
		os.RemoveAll(esxiFilePath)
		return err
	}

	err = os.MkdirAll(isoWriteRoot, 0777)
	if err != nil {
		s.logger.Error("failed to create output root directory", zap.Error(err))
		return err
	}

	err = os.Chmod(isoWriteRoot, 0777)
	if err != nil {
		s.logger.Error("failed to change permissions of the output root directory", zap.Error(err))
		return err
	}

	err = os.MkdirAll(biosBootDir, 0777)
	if err != nil {
		s.logger.Error("failed to create output root directory", zap.Error(err))
		return err
	}

	err = os.Chmod(biosBootDir, 0777)
	if err != nil {
		s.logger.Error("failed to change permissions of the output root directory", zap.Error(err))
		return err
	}

	err = copyDir(isoRead, isoWrite)
	if err != nil {
		s.logger.Error("failed to copy directory", zap.Error(err))
		return err
	}

	err = exec.Command("umount", isoRead).Run()
	if err != nil {
		s.logger.Error("failed to umount iso file", zap.Error(err))
		return err
	}

	err = os.RemoveAll(isoRead)
	if err != nil {
		s.logger.Error("failed to remove iso_read directory", zap.Error(err))
		return err
	}

	err = copyBootFiles(config, isoWriteRoot, filename)
	if err != nil {
		s.logger.Error("failed to copy boot files", zap.Error(err))
		return err
	}
	return nil
}

func copyBootFiles(config *config.Config, src, filename string) error {
	bootcfgPath := filepath.Join(src, "esxi/efi/boot/boot.cfg")
	mbootPath := filepath.Join(src, "esxi/efi/boot/bootx64.efi")
	biosBootCfgPath := filepath.Join(src, "esxi/boot.cfg")

	pxelinuxcfgPath := "templates/pxelinuxcfg"
	pxelinux0Path := "templates/pxelinux.0"

	filesToCopy := map[string]string{
		bootcfgPath:     "boot.cfg",
		mbootPath:       "mboot.efi",
		biosBootCfgPath: "bios_boot.cfg",
	}
	embedToCopy := map[string]string{
		pxelinuxcfgPath:	"pxelinux.cfg/default",
		pxelinux0Path:		"pxelinux.0",
	}

	// copy embed files
	for srcFileName, dstFileName := range embedToCopy {
		srcFileContent, err := pxelinux.ReadFile(srcFileName)
		if err != nil {
			return err
		}

		if err = os.WriteFile(filepath.Join(src, dstFileName), srcFileContent, 0666); err != nil {
				return err
		}
	}

	// copy uploaded boot files
	for srcPath, dstName := range filesToCopy {
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstPath := filepath.Join(src, dstName)
		dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0777)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		if srcPath == bootcfgPath {
			content, err := io.ReadAll(srcFile)
			if err != nil {
				return err
			}

			lines := strings.Split(string(content), "\n")
			prefixFound := false
			for i, line := range lines {
				if strings.HasPrefix(line, "kernelopt=") {
					lines[i] = fmt.Sprintf("kernelopt=runweasel ks=http://%s:%d/ks", config.ServicePortAddr, config.APIServerPort)
				} else if strings.HasPrefix(line, "prefix=") {
					lines[i] = fmt.Sprintf("prefix=%s/esxi", filename)
					prefixFound = true
				} else {
					lines[i] = strings.ReplaceAll(line, "/", "")
				}
			}
			if !prefixFound {
				newLine := fmt.Sprintf("prefix=%s/esxi", filename)
				lines = append(lines, newLine)
			}
			newContent := strings.Join(lines, "\n")
			_, err = dstFile.WriteString(newContent)
			if err != nil {
				return err
			}
		} else if srcPath == biosBootCfgPath {
			content, err := io.ReadAll(srcFile)
			if err != nil {
				return err
			}

			lines := strings.Split(string(content), "\n")
			prefixFound := false
			for i, line := range lines {
				if strings.HasPrefix(line, "kernelopt=") {
					lines[i] = fmt.Sprintf("kernelopt=runweasel ks=http://%s:%d/ks", config.ServicePortAddr, config.APIServerPort)
				} else if strings.HasPrefix(line, "prefix=") {
					lines[i] = "prefix=esxi"
					prefixFound = true
				} else {
					lines[i] = strings.ReplaceAll(line, "/", "")
				}
			}
			if !prefixFound {
				newLine := "prefix=esxi"
				lines = append(lines, newLine)
			}
			newContent := strings.Join(lines, "\n")
			_, err = dstFile.WriteString(newContent)
			if err != nil {
				return err
			}
		} else {
			_, err = io.Copy(dstFile, srcFile)
			if err != nil {
				return err
			}
		}

		err = os.Chmod(dstPath, 0777)
		if err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	_, err := os.Stat(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dst, os.ModePerm)
	if err != nil {
		return err
	}

	err = os.Chmod(dst, 0777)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			err = copyDir(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(srcPath, dstPath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	err = os.Chmod(dst, 0777)
	if err != nil {
		return err
	}

	return nil
}
