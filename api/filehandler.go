package api

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"kickstart/common"
	"kickstart/config"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/kdomanski/iso9660"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

func validateMetadata(xmlfile *iso9660.File) (*common.YamlProduct, error) {
	xmlData, err := io.ReadAll(xmlfile.Reader())
	if err != nil {
		return nil, fmt.Errorf("failed to read XML Data: %v", err)
	}

	var vum common.Vum
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	err = decoder.Decode(&vum)
	if err != nil {
		return nil, fmt.Errorf("failed to parse XML.")
	}

	if vum.Product.EsxName == "" {
		return nil, fmt.Errorf("ESX Version is not present in XML.")
	}
	esxiInfo := &common.YamlProduct{
		EsxVersion:     vum.Product.EsxVersion,
		EsxReleaseDate: vum.Product.EsxReleaseDate,
	}
	return esxiInfo, nil
}

func validateESXiISOFile(root *iso9660.File) (*common.YamlProduct, error) {
	children, err := root.GetChildren()
	if err != nil {
		return nil, err
	}
	for _, c := range children {
		if c.Name() == "UPGRADE" {
			upgrade, err := c.GetChildren()
			if err != nil {
				return nil, err
			}
			for _, cc := range upgrade {
				if cc.Name() == "METADATA.XML" {
					esxiInfo, err := validateMetadata(cc)
					if err != nil {
						return nil, err
					}
					return esxiInfo, nil
				}
			}
			return nil, fmt.Errorf("METADATA.XML not found")
		}
	}
	return nil, fmt.Errorf("UPGRADE directory not found")
}

func bootLoaderValidation(esxiInfo *common.YamlProduct, currentEsxiInfoFilePath string) bool {
	file, err := os.Open(currentEsxiInfoFilePath)
	defer file.Close()
	if err != nil {
		return true
	}

	var currentLatestEsxiInfo common.YamlProduct
	if err := yaml.NewDecoder(file).Decode(&currentLatestEsxiInfo); err != nil {
		return true
	}

	oldVersion, err := semver.NewVersion(currentLatestEsxiInfo.EsxVersion)
	if err != nil {
		return true
	}
	newVersion, err := semver.NewVersion(esxiInfo.EsxVersion)
	if err != nil {
		return false
	}
	switch {
	case newVersion.GreaterThan(oldVersion):
		return true
	case newVersion.Equal(oldVersion):
		oldDate, err := time.Parse(time.RFC3339, currentLatestEsxiInfo.EsxReleaseDate)
		if err != nil {
			return true
		}
		newDate, err := time.Parse(time.RFC3339, esxiInfo.EsxReleaseDate)
		if err != nil {
			return false
		}
		return newDate.After(oldDate)
	default:
		return false
	}
}

func (s *Server) ExtractISOfiles(config *config.Config, esxiFilePath, filename string) (err error) {
	common.IsoFileUploadMutex.Lock()
	defer common.IsoFileUploadMutex.Unlock()
	sourceISO := esxiFilePath
	bootFileDir := s.FileRootDirInfo.BootFileDirPath
	isoWriteRoot := filepath.Join(bootFileDir, filename)
	isoWrite := filepath.Join(isoWriteRoot, "esxi")

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

	esxiInfo, err := validateESXiISOFile(root)
	if err != nil {
		s.logger.Error("failed to validate iso file", zap.Error(err))
		return err
	}

	currentEsxiInfoFilePath := filepath.Join(s.FileRootDirInfo.BootFileDirPath, "latest_release.yaml")

	needUpdateMboot := bootLoaderValidation(esxiInfo, currentEsxiInfoFilePath)
	if needUpdateMboot {
		newLatestEsxiInfo, err := yaml.Marshal(esxiInfo)
		if err != nil {
			s.logger.Error("failed to read yaml", zap.Error(err))
		}
		err = ioutil.WriteFile(currentEsxiInfoFilePath, newLatestEsxiInfo, 0644)
		if err != nil {
			s.logger.Error("failed to update yaml", zap.Error(err))
			return err
		}
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

	if err = ExtractImageToDirectory(f, isoWrite); err != nil {
		s.logger.Error("failed to extract image", zap.Error(err))
		return err
	}

	err = copyBootFiles(config, bootFileDir, isoWriteRoot, filename, needUpdateMboot)
	if err != nil {
		s.logger.Error("failed to copy boot files", zap.Error(err))
		return err
	}
	return nil
}

func copyBootFiles(config *config.Config, bootFileDir, src, filename string, needUpdateMboot bool) error {
	bootcfgPath := filepath.Join(src, "esxi/efi/boot/boot.cfg")
	mbootPath := filepath.Join(src, "esxi/efi/boot/bootx64.efi")

	filesToCopy := map[string]string{
		bootcfgPath: "boot.cfg",
		mbootPath:   "mboot.efi",
	}

	for srcPath, dstName := range filesToCopy {
		switch srcPath {
		case bootcfgPath:
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			defer srcFile.Close()

			dstPath := filepath.Join(src, dstName)
			dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer dstFile.Close()

			prefixPath := `prefix=http://{{.KSServerAddr}}:{{.KSServerPort}}/installer/{{.Filename}}/esxi`
			kerneloptPath := `kernelopt=runweasel ks=http://{{.KSServerAddr}}:{{.KSServerPort}}/ks`
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
			err = os.Chmod(dstPath, 0644)
			if err != nil {
				return err
			}

		case mbootPath:
			if needUpdateMboot {
				srcFileContent, err := ioutil.ReadFile(srcPath)
				if err != nil {
					return err
				}

				common.MbootMutex.Lock()
				defer common.MbootMutex.Unlock()
				if err = os.WriteFile(filepath.Join(bootFileDir, dstName), srcFileContent, 0644); err != nil {
					return err
				}
			}
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
	  $imageName = $imageName | Sort-Object { $_.Length } | Select-Object -First 1
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
