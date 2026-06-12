module github.com/rakunlabs/turna

go 1.26

require (
	github.com/MicahParks/keyfunc/v2 v2.0.3
	github.com/abbot/go-http-auth v0.4.1-0.20220112235402-e1cee1c72f2f
	github.com/ajg/form v1.5.1
	github.com/bmatcuk/doublestar/v4 v4.9.2
	github.com/crewjam/saml v0.5.1
	github.com/dgraph-io/badger/v4 v4.5.0
	github.com/dgraph-io/ristretto/v2 v2.0.0
	github.com/dustin/go-humanize v1.0.1
	github.com/expr-lang/expr v1.16.9
	github.com/fullstorydev/grpcui v1.5.0
	github.com/go-ldap/ldap/v3 v3.4.10
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/jackc/pgx/v5 v5.10.0
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/lib/pq v1.12.3
	github.com/oklog/ulid/v2 v2.1.0
	github.com/rakunlabs/ada v0.4.4
	github.com/rakunlabs/ada/handler/swagger v0.4.4
	github.com/rakunlabs/ada/middleware/auth v0.4.4
	github.com/rakunlabs/ada/middleware/encoding v0.4.4
	github.com/rakunlabs/ada/middleware/ratelimit v0.4.4
	github.com/rakunlabs/cache v0.3.3
	github.com/rakunlabs/cache/store/redis v0.1.0
	github.com/rakunlabs/chu v0.4.7
	github.com/rakunlabs/chu/loader/external/loaderawssecrets v0.0.0-20260529215824-15c48ce668ec
	github.com/rakunlabs/chu/loader/external/loaderawsssm v0.0.0-20260529215824-15c48ce668ec
	github.com/rakunlabs/chu/loader/external/loaderazurekeyvault v0.0.0-20260529215824-15c48ce668ec
	github.com/rakunlabs/chu/loader/external/loaderconsul v0.0.0-20260529203529-25beb0ba3ee7
	github.com/rakunlabs/chu/loader/external/loadergcpparameter v0.0.0-20260529215824-15c48ce668ec
	github.com/rakunlabs/chu/loader/external/loadergcpsecret v0.0.0-20260529215824-15c48ce668ec
	github.com/rakunlabs/chu/loader/external/loadervault v0.0.0-20260529203529-25beb0ba3ee7
	github.com/rakunlabs/into v0.5.3
	github.com/rakunlabs/logi v0.4.5
	github.com/rakunlabs/muz v0.2.5
	github.com/rakunlabs/ok v0.1.0
	github.com/rakunlabs/query v0.4.6
	github.com/redis/go-redis/v9 v9.18.0
	github.com/russellhaering/goxmldsig v1.4.0
	github.com/rytsh/liz/loader v0.2.5
	github.com/rytsh/mugo v0.9.2
	github.com/spf13/cast v1.10.0
	github.com/spf13/cobra v1.10.1
	github.com/spf13/pflag v1.0.10
	github.com/swaggo/http-swagger/v2 v2.0.2
	github.com/things-go/go-socks5 v0.0.5
	github.com/timshannon/badgerhold/v4 v4.0.3
	github.com/twmb/tlscfg v1.3.0
	github.com/worldline-go/conn v0.1.0
	github.com/worldline-go/struct2 v1.4.0
	github.com/worldline-go/types v0.5.6
	github.com/xhit/go-str2duration/v2 v2.1.0
	golang.org/x/crypto v0.50.0
	golang.org/x/exp v0.0.0-20250808145144-a408d31f581a
	golang.org/x/oauth2 v0.36.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go/auth v0.18.2 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.6.0 // indirect
	cloud.google.com/go/parametermanager v0.4.0 // indirect
	cloud.google.com/go/secretmanager v1.16.0 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.21.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets v1.5.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/internal v1.2.0 // indirect
	github.com/Azure/go-ntlmssp v0.0.0-20221128193559-754e69321358 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.7.0 // indirect
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Masterminds/sprig/v3 v3.3.0 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.8 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.19 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.18 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.25 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.1.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssm v1.68.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.2 // indirect
	github.com/aws/smithy-go v1.25.1 // indirect
	github.com/beevik/etree v1.5.0 // indirect
	github.com/bufbuild/protocompile v0.10.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cli/safeexec v1.0.1 // indirect
	github.com/cncf/xds/go v0.0.0-20251210132809-ee656c7534f5 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.36.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fullstorydev/grpcurl v1.9.1 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.7 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/goccy/go-yaml v1.18.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/gomarkdown/markdown v0.0.0-20250810172220-2e2c11897d1a // indirect
	github.com/google/flatbuffers v24.3.25+incompatible // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.20.0 // indirect
	github.com/hashicorp/consul/api v1.33.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-envparse v0.1.0 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/hashicorp/vault/api v1.22.0 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jaswdr/faker v1.19.1 // indirect
	github.com/jhump/protoreflect v1.16.0 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lmittmann/tint v1.1.2 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/rakunlabs/tummy v0.1.2 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/rytsh/liz/consul v0.1.0 // indirect
	github.com/rytsh/liz/file v0.1.4 // indirect
	github.com/rytsh/liz/mapx v0.2.0 // indirect
	github.com/rytsh/liz/shutdown v0.1.0 // indirect
	github.com/rytsh/liz/templatex v0.1.0 // indirect
	github.com/rytsh/liz/vault v0.1.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	github.com/swaggo/files/v2 v2.0.0 // indirect
	github.com/swaggo/swag v1.16.6 // indirect
	github.com/tdewolff/minify/v2 v2.24.3 // indirect
	github.com/tdewolff/parse/v2 v2.8.3 // indirect
	github.com/urfave/cli/v2 v2.3.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	google.golang.org/api v0.273.1 // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401001100-f93e5f3e9f0f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260319201613-d00831a3d3e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

tool github.com/swaggo/swag/cmd/swag
