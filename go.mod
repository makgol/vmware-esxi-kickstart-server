module kickstart

go 1.19

require (
	github.com/Masterminds/semver/v3 v3.2.1
	github.com/diskfs/go-diskfs v1.4.0
	github.com/go-ozzo/ozzo-validation v3.6.0+incompatible
	github.com/gorilla/mux v1.8.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/pin/tftp/v3 v3.0.0
	go.uber.org/zap v1.24.0
	go.universe.tf/netboot v0.0.0-20230225040044-0e2ca55deb50
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496 // indirect
	github.com/elliotwutingfeng/asciiset v0.0.0-20230602022725-51bbb787efab // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/josharian/native v1.0.0 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.0.0 // indirect
	github.com/mdlayher/socket v0.2.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.17 // indirect
	github.com/pkg/xattr v0.4.9 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/stretchr/testify v1.8.2 // indirect
	github.com/ulikunitz/xz v0.5.11 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	golang.org/x/net v0.9.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	gopkg.in/djherbis/times.v1 v1.3.0 // indirect
)

replace github.com/diskfs/go-diskfs => github.com/makgol/go-diskfs v0.0.0-20230823152834-d8e8b129acd7
