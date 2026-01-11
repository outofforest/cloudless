module build

go 1.25

replace github.com/outofforest/cloudless => ../

require (
	github.com/outofforest/build/v2 v2.6.2
	github.com/outofforest/cloudless v0.0.0
	github.com/outofforest/tools v1.4.3
	github.com/samber/lo v1.52.0
)

require (
	cloud.google.com/go/auth v0.15.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.7 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.17.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.8.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.4.0 // indirect
	github.com/DataDog/zstd v1.5.5 // indirect
	github.com/ProtonMail/go-crypto v1.1.3 // indirect
	github.com/alecthomas/units v0.0.0-20240927000941-0f3dac36c52b // indirect
	github.com/aws/aws-sdk-go v1.55.6 // indirect
	github.com/beevik/ntp v1.4.3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cavaliergopher/cpio v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.3.8 // indirect
	github.com/dennwc/varint v1.0.0 // indirect
	github.com/digitalocean/go-libvirt v0.0.0-20250207191401-950a7b2d7eaf // indirect
	github.com/diskfs/go-diskfs v1.5.0 // indirect
	github.com/djherbis/times v1.6.0 // indirect
	github.com/elliotwutingfeng/asciiset v0.0.0-20240214025120-24af97c84155 // indirect
	github.com/emersion/go-imap v1.2.1 // indirect
	github.com/emersion/go-message v0.18.2 // indirect
	github.com/emersion/go-msgauth v0.6.8 // indirect
	github.com/emersion/go-sasl v0.0.0-20241020182733-b788ff22d5a6 // indirect
	github.com/emersion/go-smtp v0.24.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/nftables v0.3.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/googleapis/gax-go/v2 v2.14.1 // indirect
	github.com/gorilla/handlers v1.5.2 // indirect
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mdlayher/genetlink v1.3.2 // indirect
	github.com/mdlayher/netlink v1.7.3-0.20250113171957-fbb4dce95f42 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/outofforest/archive v0.5.0 // indirect
	github.com/outofforest/ioc/v2 v2.5.2 // indirect
	github.com/outofforest/libexec v0.3.9 // indirect
	github.com/outofforest/logger v0.6.0 // indirect
	github.com/outofforest/mass v0.2.1 // indirect
	github.com/outofforest/parallel v0.2.3 // indirect
	github.com/outofforest/proton v0.15.0 // indirect
	github.com/outofforest/resonance v0.21.0 // indirect
	github.com/outofforest/run v0.8.0 // indirect
	github.com/outofforest/spin v0.3.1 // indirect
	github.com/outofforest/varuint64 v0.1.1 // indirect
	github.com/outofforest/wave v0.1.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.7 // indirect
	github.com/pkg/xattr v0.4.10 // indirect
	github.com/prometheus/client_golang v1.21.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/prometheus/prometheus v0.301.0 // indirect
	github.com/prometheus/sigv4 v0.1.2 // indirect
	github.com/ridge/must v0.6.0 // indirect
	github.com/sassoftware/go-rpmutils v0.4.0 // indirect
	github.com/sirupsen/logrus v1.9.4-0.20230606125235-dd1b4c2e81af // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/vishvananda/netlink v1.3.0 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/wneessen/go-mail v0.6.2 // indirect
	github.com/wneessen/go-mail-middleware v0.0.9 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/collector/pdata v1.26.0 // indirect
	go.opentelemetry.io/collector/semconv v0.120.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.59.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.59.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/oauth2 v0.26.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/time v0.10.0 // indirect
	google.golang.org/api v0.222.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250219182151-9fdb1cabc7b2 // indirect
	google.golang.org/grpc v1.70.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apimachinery v0.32.2 // indirect
	k8s.io/client-go v0.32.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/utils v0.0.0-20241210054802-24370beab758 // indirect
)
