package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krateoplatformops/snowplow/plumbing/env"
	genctrl "github.com/krateoplatformops/unstructured-runtime"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	"github.com/krateoplatformops/unstructured-runtime/pkg/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	restResources "github.com/krateoplatformops/rest-dynamic-controller/internal/controllers"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"
	"github.com/krateoplatformops/unstructured-runtime/pkg/controller"
	"github.com/krateoplatformops/unstructured-runtime/pkg/pluralizer"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	serviceName = "rest-dynamic-controller"
)

var (
	Build string
)

func main() {
	// Flags
	kubeconfig := flag.String("kubeconfig", env.String("KUBECONFIG", ""),
		"absolute path to the kubeconfig file")
	debug := flag.Bool("debug",
		env.Bool("REST_CONTROLLER_DEBUG", false), "dump verbose output")
	workers := flag.Int("workers", env.Int("REST_CONTROLLER_WORKERS", 1), "number of workers")
	resyncInterval := flag.Duration("resync-interval",
		env.Duration("REST_CONTROLLER_RESYNC_INTERVAL", time.Minute*1), "resync interval")
	resourceGroup := flag.String("group",
		env.String("REST_CONTROLLER_GROUP", ""), "resource api group")
	resourceVersion := flag.String("version",
		env.String("REST_CONTROLLER_VERSION", ""), "resource api version")
	resourceName := flag.String("resource",
		env.String("REST_CONTROLLER_RESOURCE", ""), "resource plural name")
	namespace := flag.String("namespace",
		env.String("REST_CONTROLLER_NAMESPACE", ""), "namespace to watch, empty for all namespaces")
	maxErrorRetryInterval := flag.Duration("max-error-retry-interval",
		env.Duration("REST_CONTROLLER_MAX_ERROR_RETRY_INTERVAL", 30*time.Second), "The maximum interval between retries when an error occurs. This should be less than the half of the resync interval.")
	minErrorRetryInterval := flag.Duration("min-error-retry-interval",
		env.Duration("REST_CONTROLLER_MIN_ERROR_RETRY_INTERVAL", 1*time.Second), "The minimum interval between retries when an error occurs. This should be less than max-error-retry-interval.")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	zl := zap.New(zap.UseDevMode(*debug))
	log := logging.NewLogrLogger(zl.WithName(serviceName))

	// Kubernetes configuration
	var cfg *rest.Config
	var err error
	if len(*kubeconfig) > 0 {
		cfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Debug("Building kubeconfig", "error", err)
		return
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Debug("Creating dynamic client.", "error", err)
		return
	}

	discovery, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Debug("Creating discovery client.", "error", err)
		return
	}

	cachedDisc := memory.NewMemCacheClient(discovery)

	var handler controller.ExternalClient

	pluralizer := pluralizer.New()
	var swg getter.Getter
	swg, err = getter.Dynamic(cfg, pluralizer)
	if err != nil {
		log.Debug("Creating chart url info getter.", "error", err)
		return
	}

	log.WithValues("build", Build).
		WithValues("debug", *debug).
		WithValues("resyncInterval", *resyncInterval).
		WithValues("group", *resourceGroup).
		WithValues("version", *resourceVersion).
		WithValues("resource", *resourceName).
		WithValues("namespace", *namespace).
		WithValues("maxErrorRetryInterval", *maxErrorRetryInterval).
		WithValues("minErrorRetryInterval", *minErrorRetryInterval).
		WithValues("workers", *workers).
		Info("Starting.", "serviceName", serviceName)

	handler = restResources.NewHandler(cfg, log, swg, *pluralizer)
	if handler == nil {
		log.Debug("Creating handler for controller.", "error", "handler is nil")
		return
	}

	controller := genctrl.New(genctrl.Options{
		Discovery:      cachedDisc,
		Client:         dyn,
		ResyncInterval: *resyncInterval,
		GVR: schema.GroupVersionResource{
			Group:    *resourceGroup,
			Version:  *resourceVersion,
			Resource: *resourceName,
		},
		Namespace:         *namespace,
		Config:            cfg,
		Debug:             *debug,
		Logger:            log,
		ProviderName:      serviceName,
		ListWatcher:       controller.ListWatcherConfiguration{},
		Pluralizer:        *pluralizer,
		GlobalRateLimiter: workqueue.NewExponentialTimedFailureRateLimiter[any](*minErrorRetryInterval, *maxErrorRetryInterval),
	})
	controller.SetExternalClient(handler)

	ctx, cancel := signal.NotifyContext(context.Background(), []os.Signal{
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGKILL,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	}...)
	defer cancel()

	err = controller.Run(ctx, *workers)
	if err != nil {
		log.Debug("Running controller.", "error", err)
		return
	}
}
