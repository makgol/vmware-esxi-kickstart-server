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
	"path/filepath"
	"strings"
	"time"
	"bufio"
	"reflect"
	"io/fs"

	"github.com/Masterminds/semver/v3"
	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)


func rhelValidateMetadata(path string, fs filesystem.FileSystem) (*common.YamlProduct, error) {
	file, err := fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

    treeinfoData, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read treeinfo Data: %v", err)
	}
    
	var family, version string
    scanner := bufio.NewScanner(bytes.NewReader(treeinfoData))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "family =") {
			parts := strings.Split(line, " = ")
			if len(parts) > 1 {
				family = parts[1]
			}
		} else if strings.HasPrefix(line, "version =") {
			parts := strings.Split(line, " = ")
			if len(parts) > 1 {
				version = parts[1]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan treeinfo data: %v", err)
	}
	if family == "" || version == "" {
		return nil, fmt.Errorf("RHEL family or version is not present in treeinfo.")
	}
	rhelInfo := &common.YamlProduct{
		RhelFamily: family,
		RhelVersion: version,
	}
	return rhelInfo, nil
}

func validateMetadata(path string, fs filesystem.FileSystem) (*common.YamlProduct, error) {
	file, err := fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

    xmlData, err := io.ReadAll(file)
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

func validateESXiISOFile(path string, fs filesystem.FileSystem) (*common.YamlProduct, error) {
    files, err := fs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		switch strings.ToLower(file.Name()) {
        case "upgrade":
			fullpath := filepath.Join(path, file.Name())
			cfiles, err := fs.ReadDir(fullpath)
			if err != nil {
				return nil, err
			}
			for _, cfile := range cfiles {
				if strings.ToLower(cfile.Name()) == "metadata.xml" {
					cfullpath := filepath.Join(fullpath, cfile.Name())
					esxiInfo, err := validateMetadata(cfullpath, fs)
					if err != nil {
						return nil, err
					}
					return esxiInfo, nil
				}
			}
		case ".treeinfo":
			fullpath := filepath.Join(path, file.Name())
			rhelInfo, err := rhelValidateMetadata(fullpath, fs)
			if err != nil {
				return nil, err
			}
			return rhelInfo, nil
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

	f, err := diskfs.Open(sourceISO)
	if err != nil {
		s.logger.Error("failed to open iso file", zap.Error(err))
		return err
	}
	fmt.Println(reflect.TypeOf(f))

	img, err := f.GetFilesystem(0)
	if err != nil {
		s.logger.Error("failed to load iso file", zap.Error(err))
		return err
	}

	esxiInfo, err := validateESXiISOFile("/" ,img)
	if err != nil {
		s.logger.Error("failed to validate iso file", zap.Error(err))
		return err
	}
	fmt.Println(esxiInfo.EsxVersion)
	fmt.Println(esxiInfo.EsxReleaseDate)
    fmt.Println(esxiInfo.RhelFamily)
    fmt.Println(esxiInfo.RhelVersion)
    if esxiInfo.EsxVersion != ""{
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

	    if err = extractImages("/", isoWrite, img); err != nil {
	        s.logger.Error("failed to extract image", zap.Error(err))
	        return err
	    }

    	err = copyBootFiles(config, bootFileDir, isoWriteRoot, filename, needUpdateMboot)
	    if err != nil {
		    s.logger.Error("failed to copy boot files", zap.Error(err))
		    return err
	    }
	} else if esxiInfo.RhelFamily != ""{
		bootFileDir = s.FileRootDirInfo.RhelBootFileDirPath
		isoWriteRoot = filepath.Join(bootFileDir, filename)
		isoWrite =  filepath.Join(isoWriteRoot, "rhel")

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

		if err = extractImages("/", isoWrite, img); err != nil {
	        s.logger.Error("failed to extract image", zap.Error(err))
	        return err
	    }

		
		err = copyRhelFiles(config, bootFileDir, isoWriteRoot, filename)
	    if err != nil {
		    s.logger.Error("failed to copy boot files", zap.Error(err))
		    return err
	    }
	}
	return nil
}

func copyRhelFiles(config *config.Config, bootFileDir, src, filename string) error {
 	bootx64Path := filepath.Join(src, "rhel/efi/boot/bootx64.efi")
	grubx64Path := filepath.Join(src, "rhel/efi/boot/grubx64.efi")

	filesToCopy := map[string]string{
		bootx64Path: "bootx64.efi",
		grubx64Path: "grubx64.efi",
	}

	for srcPath, dstName := range filesToCopy {
		dstPath := filepath.Join(src, dstName)
		srcFileContent, err := ioutil.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err = os.WriteFile(dstPath, srcFileContent, 0644); err != nil {
			return err
		}
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

func extractImages(path, targetPath string, fs filesystem.FileSystem) error {
	err := os.MkdirAll(targetPath, 0755)
	if err != nil {
		return err
	}
	err = os.Chmod(targetPath, 0755)
	if err != nil {
		return err
	}
	fmt.Println(targetPath)
	files, err := fs.ReadDir(path)
	if err != nil {
		return err
	}
	for _, file := range files {
		newTargetPath := filepath.Join(targetPath, strings.ToLower(file.Name()))
		extractImagesHandler(file, path, newTargetPath, fs)
	}
    return nil
}

func extractImagesHandler(f fs.FileInfo, basePath, targetPath string, fs filesystem.FileSystem) error {
	basePath = filepath.Join(basePath, f.Name())
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
		children, err := fs.ReadDir(basePath)
		if err != nil {
			return err
		}
		for _, cfile := range children {
			newTargetPath := filepath.Join(targetPath, strings.ToLower(cfile.Name()))
			err = extractImagesHandler(cfile, basePath, newTargetPath, fs)
			if err != nil {
				return err
			}
		}

	} else {
		newFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		defer newFile.Close()
		file, err := fs.OpenFile(basePath, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(newFile, file)
		if err != nil {
			return err
		}
	}
	return nil
}
