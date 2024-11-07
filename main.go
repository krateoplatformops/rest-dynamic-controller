package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/client"
	helmComposition "github.com/krateoplatformops/rest-dynamic-controller/internal/composition/helmComposition"
	restComposition "github.com/krateoplatformops/rest-dynamic-controller/internal/composition/restComposition"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/controller"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/eventrecorder"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/shortid"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/support"
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/helmchart/archive"
	getter "github.com/krateoplatformops/unstructured-runtime/pkg/tools/restclient"
	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	serviceName = "composition-dynamic-controller"
)

var (
	Build string
)

func main() {
	// Flags
	kubeconfig := flag.String("kubeconfig", support.EnvString("KUBECONFIG", ""),
		"absolute path to the kubeconfig file")
	debug := flag.Bool("debug",
		support.EnvBool("COMPOSITION_CONTROLLER_DEBUG", false), "dump verbose output")
	workers := flag.Int("workers", support.EnvInt("COMPOSITION_CONTROLLER_WORKERS", 1), "number of workers")
	resyncInterval := flag.Duration("resync-interval",
		support.EnvDuration("COMPOSITION_CONTROLLER_RESYNC_INTERVAL", time.Minute*1), "resync interval")
	resourceGroup := flag.String("group",
		support.EnvString("COMPOSITION_CONTROLLER_GROUP", ""), "resource api group")
	resourceVersion := flag.String("version",
		support.EnvString("COMPOSITION_CONTROLLER_VERSION", ""), "resource api version")
	resourceName := flag.String("resource",
		support.EnvString("COMPOSITION_CONTROLLER_RESOURCE", ""), "resource plural name")
	namespace := flag.String("namespace",
		support.EnvString("COMPOSITION_CONTROLLER_NAMESPACE", "default"), "namespace")
	chart := flag.String("chart",
		support.EnvString("COMPOSITION_CONTROLLER_CHART", ""), "chart")
	cliType := flag.String("client",
		support.EnvString("COMPOSITION_CLIENT_TYPE", string(client.ClientHelm)), "client type [REST|HELM]]")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()
	clientType, err := client.ToClientType(*cliType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Initialize the logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Default level for this log is info, unless debug flag is present
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.TimeFieldFormat = time.RFC3339

	zerolog.TimeFieldFormat = time.RFC3339
	// outLogger := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339, NoColor: true, }
	log := zerolog.New(os.Stdout).With().
		Str("service", serviceName).
		Timestamp().
		Logger()

	// Kubernetes configuration
	var cfg *rest.Config
	if len(*kubeconfig) > 0 {
		cfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatal().Err(err).Msg("Building kubeconfig.")
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Creating dynamic client.")
	}

	rec, err := eventrecorder.Create(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Creating event recorder.")
	}

	var handler controller.ExternalClient
	switch clientType {
	case client.ClientREST:
		var swg getter.Getter
		swg, err = getter.Dynamic(cfg)
		if err != nil {
			log.Fatal().Err(err).Msg("Creating chart url info getter.")
		}
		handler = restComposition.NewHandler(cfg, &log, swg)
	case client.ClientHelm:
		var pig archive.Getter
		if len(*chart) > 0 {
			pig = archive.Static(*chart)
		} else {
			pig, err = archive.Dynamic(cfg, *debug)
			if err != nil {
				log.Fatal().Err(err).Msg("Creating chart url info getter.")
			}
		}
		handler = helmComposition.NewHandler(cfg, &log, pig)
	}

	log.Info().
		Str("build", Build).
		Bool("debug", *debug).
		Dur("resyncInterval", *resyncInterval).
		Str("group", *resourceGroup).
		Str("version", *resourceVersion).
		Str("resource", *resourceName).
		Str("clientType", clientType.String()).
		Msgf("Starting %s.", serviceName)

	sid, err := shortid.New(1, shortid.DefaultABC, 2342)
	if err != nil {
		log.Fatal().Err(err).Msg("Creating shortid generator.")
	}
	ctrl := controller.New(sid, controller.Options{
		Client:         dyn,
		ResyncInterval: *resyncInterval,
		GVR: schema.GroupVersionResource{
			Group:    *resourceGroup,
			Version:  *resourceVersion,
			Resource: *resourceName,
		},
		Namespace:      *namespace,
		Recorder:       rec,
		Logger:         &log,
		ExternalClient: handler,
	})
	// ctrl.SetExternalClient(handler)

	ctx, cancel := signal.NotifyContext(context.Background(), []os.Signal{
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGKILL,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	}...)
	defer cancel()

	err = ctrl.Run(ctx, *workers)
	if err != nil {
		log.Fatal().Err(err).Msg("Running controller.")
	}
}
