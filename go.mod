module github.com/gardener/terraformer

go 1.14

require (
	github.com/ahmetb/gen-crd-api-reference-docs v0.2.0 // indirect
	github.com/fsnotify/fsnotify v1.4.9
	github.com/gardener/gardener v1.6.3
	github.com/go-logr/logr v0.1.0
	github.com/hashicorp/go-multierror v1.0.0
	github.com/onsi/ginkgo v1.13.0
	github.com/onsi/gomega v1.10.1
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.13.0
	golang.org/x/sys v0.0.0-20200824131525-c12d262b63d8 // indirect
	k8s.io/api v0.18.4
	k8s.io/apimachinery v0.18.4
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.6.1
)

replace k8s.io/client-go => k8s.io/client-go v0.18.4
