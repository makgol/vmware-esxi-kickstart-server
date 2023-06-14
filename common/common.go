package common

import (
	"embed"
	"encoding/xml"
	"io/fs"
	"net"
	"sync"
)

var (
	MacIPMap               = make(map[string]net.IP)
	MacIPMapMutex          sync.RWMutex
	MacFileMap             = make(map[string]string)
	MacFileMapMutex        sync.RWMutex
	MacAddressManagerMutex sync.Mutex
	MbootMutex             sync.RWMutex
	IsoFileUploadMutex     sync.RWMutex
)

var (
	//go:embed templates/esxi-ks.cfg
	//go:embed templates/pxelinux.0
	//go:embed templates/ipxe.efi
	//go:embed templates/undionly.kpxe
	//go:embed templates/autoexec.ipxe
	//go:embed templates/default
	ksTemplatefiles embed.FS
)

func GetKsTemplatefiles() fs.FS {
	return ksTemplatefiles
}

type Vum struct {
	XMLName xml.Name `xml:"vum"`
	Product Product  `xml:"product"`
}

type Product struct {
	EsxVersion     string `xml:"esxVersion"`
	EsxName        string `xml:"name"`
	EsxReleaseDate string `xml:"releaseDate"`
}

type YamlProduct struct {
	EsxVersion     string `yaml:"esxVersion"`
	EsxReleaseDate string `yaml:"releaseDate"`
}
