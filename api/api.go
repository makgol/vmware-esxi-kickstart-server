package api

import (
	"bytes"
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"kickstart/common"
	"kickstart/config"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	validation "github.com/go-ozzo/ozzo-validation"
	"github.com/go-ozzo/ozzo-validation/is"
	"github.com/gorilla/mux"
	"github.com/mdlayher/arp"
	"go.uber.org/zap"
)

var (
	//go:embed templates/esxi-ks.cfg
	ksTemplatefile embed.FS
	//go:embed templates/pxe*
	pxelinux embed.FS
)
	
type KS struct {
	Macaddress  string   `json:"macaddress"`
	Password    string   `json:"password"`
	IP          string   `json:"ip"`
	Netmask     string   `json:"netmask"`
	Gateway     string   `json:"gateway"`
	Nameserver  string   `json:"nameserver"`
	Hostname    string   `json:"hostname"`
	VLANID      *int     `json:"vlanid"`
	CLI         []string `json:"cli"`
	Keyboard    string   `json:"keyboard"`
	ISOFilename string   `json:"isofilename"`
}

type Server struct {
	KSDirPath		string
	DHCPLeaseConfig *config.DHCPLeaseConfig
	FileRootDirInfo *config.FileRootDirInfo
	logger			*zap.Logger
}

func (k KS) Validate() error {
	return validation.ValidateStruct(&k,
		validation.Field(&k.Macaddress, validation.Required, is.MAC.Error("invalid mac address format")),
		validation.Field(&k.Password, validation.Required),
		validation.Field(&k.IP, validation.Required, is.IPv4.Error("invalid ipv4 address")),
		validation.Field(&k.Netmask, validation.Required, is.IP.Error("invalid subnet mask error")),
		validation.Field(&k.Gateway, validation.Required, is.IPv4.Error("invalid gateway address")),
		validation.Field(&k.Nameserver, validation.Required, is.IPv4.Error("invalid name server address")),
		validation.Field(&k.Hostname, validation.Required, is.DNSName.Error("invalid hostname")),
		validation.Field(&k.VLANID, validation.NotNil, validation.Min(0), validation.Max(4094)),
		validation.Field(&k.CLI, validation.Required),
		validation.Field(&k.Keyboard, validation.Required),
		validation.Field(&k.ISOFilename, validation.Required),
	)
}

func (s *Server) getKsConfig(w http.ResponseWriter, r *http.Request) {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		s.logger.Error("error parsing client IP address", zap.Error(err))
		http.Error(w, "invalid client IP address", http.StatusInternalServerError)
		return
	}
	ksFilePath := filepath.Join(s.KSDirPath, clientIP, "/ks.cfg")
	s.logger.Info(fmt.Sprintf("received GET request. KS file path is %s", ksFilePath))

	file, err := os.Open(ksFilePath)
	if err != nil {
		s.logger.Error("error opening file", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	io.Copy(w, file)
}

func (s Server) deleteKsConfig(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	mac := strings.Replace(id, "-", ":", -1)

	if mac == "" {
		s.logger.Error("mac address does not exist")
		http.Error(w, "mac address is required", http.StatusBadRequest)
		return
	}

	err := s.deleteMapManager(mac)
	if err != nil {
		s.logger.Error("failed to exec deleteMapManager", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}
}

func (s *Server) deleteMapManager(mac string) error {
	common.MacIPMapMutex.Lock()
	defer common.MacIPMapMutex.Unlock()
	delete(common.MacIPMap, mac)
	s.logger.Info(fmt.Sprintf("deleted macIPMap mapping for MAC %s", mac))

	common.MacFileMapMutex.Lock()
	defer common.MacFileMapMutex.Unlock()
	delete(common.MacFileMap, mac)
	s.logger.Info(fmt.Sprintf("Deleted macFileMap mapping for MAC %s", mac))

	return nil
}

func (s *Server) createKsConfig(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		s.logger.Error("invalid Content-Type received")
		http.Error(w, "invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("could not read request body", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}

	var ks KS
	err = json.Unmarshal([]byte(body), &ks)
	if err != nil {
		s.logger.Error("could not unmarshall request body", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}

	err = ks.Validate()
	if err != nil {
		s.logger.Error("validate request error", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	kscfg, err := template.ParseFS(ksTemplatefile, "templates/esxi-ks.cfg")
	if err != nil {
		s.logger.Error("failed to parse", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = s.isoFileMapManager(ks.Macaddress, ks.ISOFilename)
	if err != nil {
		s.logger.Error("error saving MAC to IsoFilename mappings", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusBadRequest)
	}

	err = s.macAddressManager(ks.Macaddress, s.DHCPLeaseConfig)
	if err != nil {
		s.logger.Error("error saving MAC to IP mappings", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusBadRequest)
	}

	ksfolder := filepath.Join(s.KSDirPath, common.MacIPMap[ks.Macaddress].String())
	err = os.MkdirAll(ksfolder, os.ModePerm)
	if err != nil {
		s.logger.Error("failed to create ks directory", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}

	file, err := os.Create(ksfolder + "/ks.cfg")
	if err != nil {
		s.logger.Error("failed to create ks config file", zap.Error(err))
		http.Error(w, "encountered unexpected problem", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	kscfg.Execute(file, ks)
}

func (s *Server) isoFileMapManager(mac, isoname string) error {
	common.MacFileMapMutex.Lock()
	defer common.MacFileMapMutex.Unlock()
	common.MacFileMap[mac] = isoname
	s.logger.Info(fmt.Sprintf("update bootFilename %s to MAC %s", isoname, mac))
	return nil
}

func (s *Server) macAddressManager(mac string, dhcpInfo *config.DHCPLeaseConfig) error {
	common.MacAddressManagerMutex.Lock()
	defer common.MacAddressManagerMutex.Unlock()

	common.MacIPMapMutex.RLock()
	ip, ok := common.MacIPMap[mac]
	common.MacIPMapMutex.RUnlock()

	if ok {
		s.logger.Info(fmt.Sprintf("MAC %s already has IP %s assigned", mac, ip))
	} else {
		availableIP := FindAvailableIP(common.MacIPMap, dhcpInfo)

		if availableIP != nil {
			updateMacToIPMap(mac, availableIP)
			s.logger.Info(fmt.Sprintf("assigned IP %s to MAC %s", availableIP, mac))
		} else {
			return errors.New("any IPs available in the specified range")
		}
	}
	return nil
}

func updateMacToIPMap(mac string, ip net.IP) {
	common.MacIPMapMutex.Lock()
	defer common.MacIPMapMutex.Unlock()
	common.MacIPMap[mac] = ip
}

func FindAvailableIP(usedIPs map[string]net.IP, dhcpConfig *config.DHCPLeaseConfig) net.IP {
	start := ipToInt(dhcpConfig.DHCPStartIP)
	end := ipToInt(dhcpConfig.DHCPEndIP)

	used := make(map[uint32]bool)
	for _, ip := range usedIPs {
		used[ipToInt(ip)] = true
	}

	for i := start; i <= end; i++ {
		if !used[i] {
			ip := intToIP(i)
			if !isIPUsed(ip, dhcpConfig.DHCPInterfaceName) {
				return ip
			}
		}
	}
	return nil
}

func ipToInt(ip net.IP) uint32 {
	ipv4Int := binary.BigEndian.Uint32(ip.To4())
	return ipv4Int
}

func intToIP(ipInt uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, ipInt)
	return ip
}

func isIPUsed(searchIP net.IP, servicePortName string) bool {
	// Ensure valid network interface
	ifi, err := net.InterfaceByName(servicePortName)
	if err != nil {
		log.Fatal(err)
	}

	// Check if the interface itself has the IP
	addrs, err := ifi.Addrs()
	if err != nil {
		log.Fatal(err)
	}
	for _, addr := range addrs {
		// Check if the address is the one we're searching for
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.Equal(searchIP) {
			return true
		}
	}

	// Set up ARP client with socket
	c, err := arp.Dial(ifi)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// Set request deadline
	if err := c.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
		log.Fatal(err)
	}

	// Request hardware address for IP address
	ip, err := netip.ParseAddr(searchIP.String())
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.Resolve(ip)
	return err == nil
}

type Response struct {
	UploadedFiles map[string]string `json:"uploaded_esxi_list"`
}

func (s *Server) esxiVersionList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		uploadedFiles := make(map[string]string)
		filepath.Walk(s.FileRootDirInfo.BootFileDirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				s.logger.Error("failed to find boot file directory", zap.Error(err))
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return err
			}
			if info.IsDir() && filepath.Dir(path) == s.FileRootDirInfo.BootFileDirPath {
				xmlPath := filepath.Join(path, "esxi", "upgrade", "metadata.xml")
				xmlFile, err := os.Open(xmlPath)
				if err != nil {
					s.logger.Error("failed to open metadata.xml", zap.Error(err))
					return err
				}
				defer xmlFile.Close()

				xmlData, err := io.ReadAll(xmlFile)
				if err != nil {
					s.logger.Error("failed to read metadata.xml", zap.Error(err))
					return err
				}

				var vum common.Vum
				decoder := xml.NewDecoder(bytes.NewReader(xmlData))
				decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
					return input, nil
				}
				err = decoder.Decode(&vum)
				if err != nil {
					s.logger.Error("failed to decode metadata.xml", zap.Error(err))
					return err
				}
				uploadedFiles[filepath.Base(path)] = vum.Product.EsxVersion
			}
			return nil
		})
		if err != nil {
			s.logger.Error("failed to read esxi version files", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(
			Response{
				UploadedFiles: uploadedFiles,
			},
		); err != nil {
			s.logger.Error("failed to generate response", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func initializeKsDir(dirPath string) (string, error) {
	ksDirPath := filepath.Join(dirPath, "ks")
	err := os.RemoveAll(ksDirPath)
	if err != nil {
		return "", fmt.Errorf("failed to remove directory: %w", err)
	}

	err = os.Mkdir(ksDirPath, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return ksDirPath, nil
}

func (s *Server) ksHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.getKsConfig(w, r)
	case "POST":
		s.createKsConfig(w, r)
	default:
		s.logger.Warn(fmt.Sprintf("method %s not allowed", r.Method))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) ksIDHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "DELETE":
		s.deleteKsConfig(w, r)
	default:
		s.logger.Warn(fmt.Sprintf("method %s not allowed", r.Method))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func RunServer(ctx context.Context, cfg *config.Config, logger *zap.Logger, fileRootDirInfo *config.FileRootDirInfo) {
	newKsDirPath, err := initializeKsDir(cfg.KsDirPath)
	if err != nil {
		logger.Error("error initializing KS directory", zap.Error(err))
		return
	}
	dhcpCfg := config.GetDHCPLeaseConfig(cfg)
	srv := &Server{
		KSDirPath:			newKsDirPath,
		DHCPLeaseConfig:	dhcpCfg,
		FileRootDirInfo:	fileRootDirInfo,
		logger:				logger,
	}
	select {
	case <- ctx.Done():
		logger.Fatal("shutting down API server...", zap.Error(err))
		return 
	default:
	}

	logger.Info("starting API server...")

	r := mux.NewRouter()

	r.HandleFunc("/", srv.uploadForm())
	r.HandleFunc("/upload", srv.getUploadFileHandler(cfg))
	r.HandleFunc("/ks", srv.ksHandler)
	r.HandleFunc("/ks/{id}", srv.ksIDHandler)
	r.HandleFunc("/esxi-versions", srv.esxiVersionList())

	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.APIServerPort), r); err != nil {
		logger.Panic("shutting down API server...", zap.Error(err))
	}
}
