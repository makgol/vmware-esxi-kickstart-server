package common

import (
	"encoding/xml"
	"net"
	"sync"
)

var (
	MacIPMap               = make(map[string]net.IP)
	MacIPMapMutex          sync.RWMutex
	MacFileMap             = make(map[string]string)
	MacFileMapMutex        sync.RWMutex
	MacAddressManagerMutex sync.Mutex
)

type Vum struct {
	XMLName xml.Name `xml:"vum"`
	Product Product  `xml:"product"`
}

type Product struct {
	EsxVersion string `xml:"esxVersion"`
	EsxName    string `xml:"name"`
}
