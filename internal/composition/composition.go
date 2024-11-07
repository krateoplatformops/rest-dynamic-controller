package composition

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/gobuffalo/flect"
	"github.com/krateoplatformops/rest-dynamic-controller/interal/tools/apiaction"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/client/restclient"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/controller"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/meta"
	getter "github.com/krateoplatformops/unstructured-runtime/pkg/tools/restclient"
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"
	"github.com/lucasepe/httplib"

	"github.com/rs/zerolog"

	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var _ controller.ExternalClient = (*handler)(nil)

func NewHandler(cfg *rest.Config, log *zerolog.Logger, swg getter.Getter) controller.ExternalClient {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Creating dynamic client.")
	}

	dis, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Creating discovery client.")
	}

	return &handler{
		logger:            log,
		dynamicClient:     dyn,
		discoveryClient:   dis,
		swaggerInfoGetter: swg,
	}
}

type handler struct {
	logger            *zerolog.Logger
	dynamicClient     dynamic.Interface
	discoveryClient   *discovery.DiscoveryClient
	swaggerInfoGetter getter.Getter
}

func (h *handler) Observe(ctx context.Context, mg *unstructured.Unstructured) (bool, error) {
	log := h.logger.With().Timestamp().
		Str("op", "Observe").
		Str("apiVersion", mg.GetAPIVersion()).
		Str("kind", mg.GetKind()).
		Str("name", mg.GetName()).
		Str("namespace", mg.GetNamespace()).Logger()

	if h.swaggerInfoGetter == nil {
		return false, fmt.Errorf("swagger file info getter must be specified")
	}
	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Err(err).Msg("Getting REST client info")
		return false, err
	}
	if clientInfo == nil {
		return false, fmt.Errorf("swagger info is nil")
	}
	tools.Update(ctx, mg, tools.UpdateOptions{
		DiscoveryClient: h.discoveryClient,
		DynamicClient:   h.dynamicClient,
	})

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Err(err).Msg("Building REST client")
		return false, err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)
	cli.IdentifierFields = clientInfo.Resource.Identifiers
	cli.SpecFields = mg
	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Err(err).Msg("Getting spec")
		return false, err
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		log.Warn().AnErr("Getting status", err)
	}
	var body *map[string]interface{}
	isKnown := isResourceKnown(cli, log, clientInfo, statusFields, specFields)

	if isKnown {
		// Getting the external resource by its identifier
		apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
		if apiCall == nil {
			log.Warn().Msgf("API call not found for %s", apiaction.Get)
			return true, nil
		}
		if err != nil {
			log.Err(err).Msg("Building API call")
			return false, err
		}
		reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
		if reqConfiguration == nil {
			return false, fmt.Errorf("error building call configuration")
		}
		body, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
		if httplib.IsNotFoundError(err) {
			log.Debug().Str("Resource", mg.GetKind()).Msg("External resource not found.")
			return false, nil
		}
		if err != nil {
			log.Err(err).Msg("Performing REST call")
			return false, err
		}
	} else {
		apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.FindBy)
		if apiCall == nil {
			if !unstructuredtools.IsConditionSet(mg, condition.Creating()) && !unstructuredtools.IsConditionSet(mg, condition.Available()) {
				log.Debug().Str("Resource", mg.GetKind()).Msg("External resource is being created.")
				return false, nil
			}
			log.Warn().Msgf("API call not found for %s", apiaction.FindBy)
			log.Warn().Msgf("Resource is assumed to be up-to-date.")
			cond := condition.Available()
			cond.Message = "Resource is assumed to be up-to-date. API call not found for FindBy."
			err = unstructuredtools.SetCondition(mg, cond)
			if err != nil {
				log.Err(err).Msg("Setting condition")
				return false, err
			}
			return true, tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
				DiscoveryClient: h.discoveryClient,
				DynamicClient:   h.dynamicClient,
			})
		}
		if err != nil {
			log.Err(err).Msg("Building API call")
			return false, err
		}
		reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
		if reqConfiguration == nil {
			return false, fmt.Errorf("error building call configuration")
		}
		body, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
		if httplib.IsNotFoundError(err) {
			log.Debug().Str("Resource", mg.GetKind()).Msg("External resource not found.")
			return false, nil
		}
		if err != nil {
			log.Err(err).Msg("Performing REST call")
			return false, err
		}
	}

	if body != nil {
		err = populateStatusFields(clientInfo, mg, body)
		if err != nil {
			log.Err(err).Msg("Updating identifiers")
			return false, err
		}

		err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
			DiscoveryClient: h.discoveryClient,
			DynamicClient:   h.dynamicClient,
		})
		if err != nil {
			log.Err(err).Msg("Updating status")
			return false, err
		}
		ok, err := isCRUpdated(mg, *body)
		if err != nil {
			log.Err(err).Msg("Checking if CR is updated")
			return false, err
		}
		if !ok {
			log.Debug().Str("Resource", mg.GetKind()).Msg("External resource not up-to-date.")
			return true, apierrors.NewNotFound(schema.GroupResource{
				Group:    mg.GroupVersionKind().Group,
				Resource: flect.Pluralize(strings.ToLower(mg.GetKind())),
			}, mg.GetName())
		}
	}
	log.Debug().Str("Resource", mg.GetKind()).Msg("Setting condition.")
	err = unstructuredtools.SetCondition(mg, condition.Available())
	if err != nil {
		log.Err(err).Msg("Setting condition")
		return false, err
	}
	err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		DiscoveryClient: h.discoveryClient,
		DynamicClient:   h.dynamicClient,
	})
	if err != nil {
		log.Err(err).Msg("Updating status")
		return false, err
	}

	log.Debug().Str("Resource", mg.GetKind()).Msg("External resource up-to-date.")

	return true, nil
}

func (h *handler) Create(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.With().Timestamp().
		Str("op", "Create").
		Str("apiVersion", mg.GetAPIVersion()).
		Str("kind", mg.GetKind()).
		Str("name", mg.GetName()).
		Str("namespace", mg.GetNamespace()).Logger()

	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Err(err).Msg("Getting REST client info")
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Err(err).Msg("Building REST client")
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Err(err).Msg("Getting spec")
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Create)
	if err != nil {
		log.Err(err).Msg("Building API call")
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, nil, specFields)
	body, err := apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Err(err).Msg("Performing REST call")
		return err
	}

	log.Debug().Str("Resource", mg.GetKind()).Msg("Creating external resource.")

	err = unstructuredtools.SetCondition(mg, condition.Creating())
	if err != nil {
		log.Err(err).Msg("Setting condition")
		return err
	}

	err = populateStatusFields(clientInfo, mg, body)
	if err != nil {
		log.Err(err).Msg("Updating identifiers")
		return err
	}

	err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		DiscoveryClient: h.discoveryClient,
		DynamicClient:   h.dynamicClient,
	})
	if err != nil {
		log.Err(err).Msg("Updating status")
		return err
	}

	return nil
}

func (h *handler) Update(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.With().
		Str("op", "Update").
		Str("apiVersion", mg.GetAPIVersion()).
		Str("kind", mg.GetKind()).
		Str("name", mg.GetName()).
		Str("namespace", mg.GetNamespace()).Logger()

	log.Debug().Msg("Handling composition values update.")
	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Err(err).Msg("Getting REST client info")
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Err(err).Msg("Building REST client")
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Err(err).Msg("Getting spec")
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Update)
	if err != nil {
		log.Err(err).Msg("Building API call")
		return err
	}

	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err == fmt.Errorf("%s not found", "status") {
		log.Debug().Str("Resource", mg.GetKind()).Msg("External resource not created yet.")
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	body, err := apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Err(err).Msg("Performing REST call")
		return err
	}

	err = populateStatusFields(clientInfo, mg, body)
	if err != nil {
		log.Err(err).Msg("Updating identifiers")
		return err
	}

	log.Debug().Str("Resource", mg.GetKind()).Msg("Creating external resource.")

	err = unstructuredtools.SetCondition(mg, condition.Creating())
	if err != nil {
		log.Err(err).Msg("Setting condition")
		return err
	}

	err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		DiscoveryClient: h.discoveryClient,
		DynamicClient:   h.dynamicClient,
	})
	if err != nil {
		log.Err(err).Msg("Updating status")
		return err
	}

	err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		DiscoveryClient: h.discoveryClient,
		DynamicClient:   h.dynamicClient,
	})
	if err != nil {
		log.Err(err).Msg("Updating status")
		return err
	}
	log.Debug().Str("kind", mg.GetKind()).Msg("Composition values updated.")

	return nil

}

func (h *handler) Delete(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.With().
		Str("op", "Delete").
		Str("apiVersion", mg.GetAPIVersion()).
		Str("kind", mg.GetKind()).
		Str("name", mg.GetName()).
		Str("namespace", mg.GetNamespace()).Logger()

	log.Debug().Msg("Handling composition values deletion.")

	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Err(err).Msg("Getting REST client info")
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Err(err).Msg("Building REST client")
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = true

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Err(err).Msg("Getting spec")
		return err
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		log.Err(err).Msg("Getting status")
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Delete)
	if apiCall == nil {
		log.Warn().Msgf("API call not found for %s", apiaction.Delete)
		return removeFinalizersAndUpdate(ctx, log, h.discoveryClient, h.dynamicClient, mg)
	}
	if err != nil {
		log.Err(err).Msg("Building API call")
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	if reqConfiguration == nil {
		return fmt.Errorf("error building call configuration")
	}

	_, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Err(err).Msg("Performing REST call")
		return err
	}

	log.Debug().Str("Resource", mg.GetKind()).Msg("Deleting external resource.")

	err = unstructuredtools.SetCondition(mg, condition.Deleting())
	if err != nil {
		log.Err(err).Msg("Setting condition")
		return err
	}

	return removeFinalizersAndUpdate(ctx, log, h.discoveryClient, h.dynamicClient, mg)
}
