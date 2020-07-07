module github.com/kedacore/keda

go 1.13

require (
	cloud.google.com/go v0.55.0
	github.com/Azure/azure-amqp-common-go/v3 v3.0.0
	github.com/Azure/azure-event-hubs-go v1.3.1
	github.com/Azure/azure-sdk-for-go v41.1.0+incompatible
	github.com/Azure/azure-service-bus-go v0.10.2
	github.com/Azure/azure-storage-blob-go v0.8.0
	github.com/Azure/azure-storage-queue-go v0.0.0-20191125232315-636801874cdd
	github.com/Azure/go-autorest/autorest v0.10.0 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.2
	github.com/Huawei/gophercloud v1.0.21
	github.com/Shopify/sarama v1.26.4
	github.com/aws/aws-sdk-go v1.32.3
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/go-logr/logr v0.1.0
	github.com/go-openapi/spec v0.19.7
	github.com/go-redis/redis v6.15.8+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/golang/mock v1.4.3
	github.com/golang/protobuf v1.3.5
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/hashicorp/vault/api v1.0.4
	github.com/imdario/mergo v0.3.9
	github.com/kubernetes-incubator/custom-metrics-apiserver v0.0.0-20200323093244-5046ce1afe6b
	github.com/lib/pq v1.3.0
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/operator-framework/operator-sdk v0.18.1
	github.com/pkg/errors v0.9.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/pflag v1.0.5
	github.com/streadway/amqp v0.0.0-20200108173154-1c71cc93ed71
	github.com/stretchr/testify v1.5.1
	github.com/tmc/grpc-websocket-proxy v0.0.0-20200122045848-3419fae592fc // indirect
	github.com/xdg/scram v0.0.0-20180814205039-7eeb5667e42c
	google.golang.org/api v0.20.0
	google.golang.org/genproto v0.0.0-20200326112834-f447254575fd
	google.golang.org/grpc v1.28.0
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/code-generator v0.18.2
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c
	k8s.io/metrics v0.18.2
	pack.ag/amqp v0.12.5 // indirect
	sigs.k8s.io/controller-runtime v0.6.0
)

// Need to use this until this PR with k8s 1.18 is merged https://github.com/kubernetes-sigs/custom-metrics-apiserver/pull/66
replace github.com/kubernetes-incubator/custom-metrics-apiserver => github.com/zroubalik/custom-metrics-apiserver v0.0.0-20200504115811-b4bb20049e83

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/apiserver => k8s.io/apiserver v0.18.2 // Required by kubernetes-incubator/custom-metrics-apiserver
	k8s.io/client-go => k8s.io/client-go v0.18.2
)

// Required to resolve go/grpc issues
replace (
	cloud.google.com/go => cloud.google.com/go v0.46.3
	google.golang.org/api => google.golang.org/api v0.10.0
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20191002211648-c459b9ce5143
	google.golang.org/grpc => google.golang.org/grpc v1.24.0
)
