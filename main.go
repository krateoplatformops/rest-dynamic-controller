package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/krateoplatformops/plumbing/ptr"
	prettylog "github.com/krateoplatformops/plumbing/slogs/pretty"
	"github.com/krateoplatformops/snowplow/plumbing/env"
	genctrl "github.com/krateoplatformops/unstructured-runtime"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	metricsserver "github.com/krateoplatformops/unstructured-runtime/pkg/metrics/server"
	"github.com/krateoplatformops/unstructured-runtime/pkg/workqueue"

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
	metricsServerPort := flag.Int("metrics-server-port",
		env.Int("REST_CONTROLLER_METRICS_SERVER_PORT", 0), "The address to bind the metrics server to. If empty, metrics server is disabled.")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}

	lh := prettylog.New(&slog.HandlerOptions{
		Level:     logLevel,
		AddSource: false,
	},
		prettylog.WithDestinationWriter(os.Stderr),
		prettylog.WithColor(),
		prettylog.WithOutputEmptyAttrs(),
	)

	log := logging.NewLogrLogger(logr.FromSlogHandler(slog.New(lh).Handler()))

	// Kubernetes configuration
	var cfg *rest.Config
	var err error
	if len(*kubeconfig) > 0 {
		cfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		cfg, err = genctrl.GetConfig()
	}
	if err != nil {
		log.Error(err, "Building kubeconfig.")
		os.Exit(1)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "Creating dynamic client.")
		os.Exit(1)
	}

	discovery, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Error(err, "Creating discovery client.")
		os.Exit(1)
	}

	cachedDisc := memory.NewMemCacheClient(discovery)

	var handler controller.ExternalClient

	pluralizer := pluralizer.New()
	var swg getter.Getter
	swg, err = getter.Dynamic(cfg, pluralizer)
	if err != nil {
		log.Error(err, "Creating definition getter.")
		os.Exit(1)
	}

	metricsServerBindAddress := ""
	if ptr.Deref(metricsServerPort, 0) != 0 {
		log.Info("Metrics server enabled", "bindAddress", fmt.Sprintf(":%d", *metricsServerPort))
		metricsServerBindAddress = fmt.Sprintf(":%d", *metricsServerPort)
	} else {
		log.Info("Metrics server disabled")
		metricsServerBindAddress = "0"
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
		log.Error(fmt.Errorf("handler is nil"), "Creating handler for controller.")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), []os.Signal{
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGKILL,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	}...)
	defer cancel()

	controller := genctrl.New(ctx, genctrl.Options{
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
		Metrics: metricsserver.Options{
			BindAddress: metricsServerBindAddress,
		},
	})
	controller.SetExternalClient(handler)

	err = controller.Run(ctx, *workers)
	if err != nil {
		log.Error(err, "Running controller.")
		os.Exit(1)
	}
}
