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
	"path"
	"path/filepath"
	"strings"

	"github.com/kdomanski/iso9660"
	"go.uber.org/zap"
)

func validateMetadata(xmlfile *iso9660.File) error {
	xmlData, err := io.ReadAll(xmlfile.Reader())
	if err != nil {
		return fmt.Errorf("failed to read XML Data: %v", err)
	}

	var vum common.Vum
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	err = decoder.Decode(&vum)
	if err != nil {
		return fmt.Errorf("failed to parse XML.")
	}

	if vum.Product.EsxName == "" {
		return fmt.Errorf("ESX Version is not present in XML.")
	}
	return nil
}

func validateESXiISOFile(root *iso9660.File) error {
	children, err := root.GetChildren()
	if err != nil {
		return err
	}
	for _, c := range children {
		if c.Name() == "UPGRADE" {
			upgrade, err := c.GetChildren()
			if err != nil {
				return err
			}
			for _, cc := range upgrade {
				if cc.Name() == "METADATA.XML" {
					err := validateMetadata(cc)
					if err != nil {
						return err
					}
					return nil
				}
			}
			return fmt.Errorf("METADATA.XML not found")
		}
	}
	return fmt.Errorf("UPGRADE directory not found")
}

func (s *Server) ExtractISOfiles(config *config.Config, esxiFilePath, filename string) (err error) {
	sourceISO := esxiFilePath
	bootFileDir := s.FileRootDirInfo.BootFileDirPath
	isoWriteRoot := filepath.Join(bootFileDir, filename)
	isoWrite := filepath.Join(isoWriteRoot, "esxi")
	biosBootDir := filepath.Join(isoWriteRoot, "pxelinux.cfg")

	defer func() {
		if err != nil {
			os.RemoveAll(isoWriteRoot)
			os.RemoveAll(sourceISO)
		}
	}()

	f, err := os.Open(sourceISO)
	if err != nil {
		s.logger.Error("failed to open iso file", zap.Error(err))
		return err
	}
	defer f.Close()

	img, err := iso9660.OpenImage(f)
	if err != nil {
		s.logger.Error("failed to load iso file", zap.Error(err))
		return err
	}

	root, err := img.RootDir()
	if err != nil {
		s.logger.Error("failed to read iso root directory", zap.Error(err))
		return err
	}

	err = validateESXiISOFile(root)
	if err != nil {
		s.logger.Error("failed to validate iso file", zap.Error(err))
		return err
	}

	err = os.MkdirAll(isoWriteRoot, 0755)
	if err != nil {
		s.logger.Error("failed to create output root directory", zap.Error(err))
		return err
	}

	err = os.Chmod(isoWriteRoot, 0755)
	if err != nil {
		s.logger.Error("failed to change permissions of the output root directory", zap.Error(err))
		return err
	}

	err = os.MkdirAll(biosBootDir, 0755)
	if err != nil {
		s.logger.Error("failed to create output root directory", zap.Error(err))
		return err
	}

	err = os.Chmod(biosBootDir, 0755)
	if err != nil {
		s.logger.Error("failed to change permissions of the output root directory", zap.Error(err))
		return err
	}

	if err = ExtractImageToDirectory(f, isoWrite); err != nil {
		s.logger.Error("failed to extract image", zap.Error(err))
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
	ipxePath := "templates/ipxe.efi"
	undionlyPath := "templates/undionly.kpxe"
	autoexecPath := "templates/autoexec.ipxe"

	filesToCopy := map[string]string{
		bootcfgPath:     "boot.cfg",
		mbootPath:       "mboot.efi",
		biosBootCfgPath: "bios_boot.cfg",
	}
	embedToCopy := map[string]string{
		pxelinuxcfgPath: "pxelinux.cfg/default",
		pxelinux0Path:   "pxelinux.0",
		ipxePath:        "ipxe.efi",
		undionlyPath:    "undionly.kpxe",
		autoexecPath:    "autoexec.ipxe",
	}

	// copy embed files
	for srcFileName, dstFileName := range embedToCopy {
		srcFileContent, err := ipxe.ReadFile(srcFileName)
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
		dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		prefixPath := fmt.Sprintf("prefix=http://%s:%d/ipxe/%s/esxi", config.ServicePortAddr, config.APIServerPort, filename)
		kerneloptPath := fmt.Sprintf("kernelopt=runweasel ks=http://%s:%d/ks", config.ServicePortAddr, config.APIServerPort)
		if srcPath == bootcfgPath {
			content, err := io.ReadAll(srcFile)
			if err != nil {
				return err
			}

			lines := strings.Split(string(content), "\n")
			prefixFound := false
			for i, line := range lines {
				if strings.HasPrefix(line, "kernelopt=") {
					lines[i] = kerneloptPath
				} else if strings.HasPrefix(line, "prefix=") {
					lines[i] = prefixPath
					prefixFound = true
				} else {
					lines[i] = strings.ReplaceAll(line, "/", "")
				}
			}
			if !prefixFound {
				newLine := prefixPath
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
					lines[i] = kerneloptPath
				} else if strings.HasPrefix(line, "prefix=") {
					lines[i] = prefixPath
					prefixFound = true
				} else {
					lines[i] = strings.ReplaceAll(line, "/", "")
				}
			}
			if !prefixFound {
				newLine := prefixPath
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

		err = os.Chmod(dstPath, 0755)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) zipToIso(config *config.Config, esxiFilePath, filename string) (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		s.logger.Error("failed to get current directory", zap.Error(err))
		return "", err
	}
	esxiFilePath = filepath.Join(currentDir, esxiFilePath)
	isoFilePath := strings.Replace(esxiFilePath, ".zip", ".iso", -1)
	commands := fmt.Sprintf(`
      $addDepo = Add-EsxSoftwareDepot %s
	  $imageName = (Get-EsxImageProfile | Where-Object { $_.Name -match '^ESXi-.*[0-9]-standard$' }).Name
	  $exportResult = Export-EsxImageProfile -ImageProfile $imageName -ExportToIso %s -Force
    `, esxiFilePath, isoFilePath)
	err = exec.Command("pwsh", "-c", commands).Run()
	if err != nil {
		s.logger.Error("Failed to exec powercli script for convert to iso from zip", zap.Error(err))
		return "", err
	}
	return isoFilePath, nil
}

func ExtractImageToDirectory(image io.ReaderAt, destination string) error {
	img, err := iso9660.OpenImage(image)
	if err != nil {
		return err
	}

	root, err := img.RootDir()
	if err != nil {
		return err
	}

	return extract(root, destination)

}

func extract(f *iso9660.File, targetPath string) error {
	if f.IsDir() {
		existing, err := os.Open(targetPath)
		if err == nil {
			defer existing.Close()
			s, err := existing.Stat()
			if err != nil {
				return err
			}

			if !s.IsDir() {
				return fmt.Errorf("%s already exists and is a file", targetPath)
			}
		} else if os.IsNotExist(err) {
			if err = os.Mkdir(targetPath, 0755); err != nil {
				return err
			}
		} else {
			return err
		}

		children, err := f.GetChildren()
		if err != nil {
			return err
		}

		for _, c := range children {
			if err = extract(c, path.Join(targetPath, strings.ToLower(c.Name()))); err != nil {
				return err
			}
		}
	} else { // it's a file
		newFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		defer newFile.Close()
		if _, err = io.Copy(newFile, f.Reader()); err != nil {
			return err
		}
	}

	return nil
}
