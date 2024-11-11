package restResources

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/gobuffalo/flect"
	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/restclient"
	"github.com/krateoplatformops/unstructured-runtime/pkg/controller"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	"github.com/krateoplatformops/unstructured-runtime/pkg/meta"
	"github.com/krateoplatformops/unstructured-runtime/pkg/pluralizer"
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"
	"github.com/lucasepe/httplib"

	"github.com/krateoplatformops/unstructured-runtime/pkg/tools"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var _ controller.ExternalClient = (*handler)(nil)

func NewHandler(cfg *rest.Config, log logging.Logger, swg getter.Getter, pluralizer pluralizer.Pluralizer) controller.ExternalClient {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Debug("Creating dynamic client", "error", err)
	}

	dis, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Debug("Creating discovery client", "error", err)
	}

	return &handler{
		pluralizer:        pluralizer,
		logger:            log,
		dynamicClient:     dyn,
		discoveryClient:   dis,
		swaggerInfoGetter: swg,
	}
}

type handler struct {
	pluralizer        pluralizer.Pluralizer
	logger            logging.Logger
	dynamicClient     dynamic.Interface
	discoveryClient   *discovery.DiscoveryClient
	swaggerInfoGetter getter.Getter
}

func (h *handler) Observe(ctx context.Context, mg *unstructured.Unstructured) (controller.ExternalObservation, error) {
	log := h.logger.WithValues("op", "Observe").
		WithValues("apiVersion", mg.GetAPIVersion()).
		WithValues("kind", mg.GetKind()).
		WithValues("name", mg.GetName()).
		WithValues("namespace", mg.GetNamespace())

	if h.swaggerInfoGetter == nil {
		return controller.ExternalObservation{}, fmt.Errorf("swagger file info getter must be specified")
	}
	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Debug("Getting REST client info", "error", err)
		return controller.ExternalObservation{}, err
	}
	if clientInfo == nil {
		log.Debug("Swagger info is nil")
		return controller.ExternalObservation{}, fmt.Errorf("swagger info is nil")
	}
	mg, err = tools.Update(ctx, mg, tools.UpdateOptions{
		Pluralizer:    h.pluralizer,
		DynamicClient: h.dynamicClient,
	})
	if err != nil {
		log.Debug("Updating CR", "error", err)
		return controller.ExternalObservation{}, err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return controller.ExternalObservation{}, err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)
	cli.IdentifierFields = clientInfo.Resource.Identifiers
	cli.SpecFields = mg
	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Debug("Getting spec", "error", err)
		return controller.ExternalObservation{}, err
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		log.Debug("Error getting status.", "error", err)
	}
	var body *map[string]interface{}
	isKnown := isResourceKnown(cli, log, clientInfo, statusFields, specFields)

	if isKnown {
		// Getting the external resource by its identifier
		apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
		if apiCall == nil {
			log.Debug("API call not found", "action", apiaction.Get)
			return controller.ExternalObservation{}, fmt.Errorf("API call not found for %s", apiaction.Get)
		}
		if err != nil {
			log.Debug("Building API call", "error", err)
			return controller.ExternalObservation{}, err
		}
		reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
		if reqConfiguration == nil {
			return controller.ExternalObservation{}, fmt.Errorf("error building call configuration")
		}
		body, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
		if httplib.IsNotFoundError(err) {
			log.Debug("External resource not found", "kind", mg.GetKind())
			return controller.ExternalObservation{
				ResourceExists:   false,
				ResourceUpToDate: false,
			}, nil
		}
		if err != nil {
			log.Debug("Performing REST call", "error", err)
			return controller.ExternalObservation{}, err
		}
	} else {
		apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.FindBy)
		if apiCall == nil {
			if !unstructuredtools.IsConditionSet(mg, condition.Creating()) && !unstructuredtools.IsConditionSet(mg, condition.Available()) {
				log.Debug("External resource is being created", "kind", mg.GetKind())
				return controller.ExternalObservation{}, nil
			}
			log.Debug("API call not found", "action", apiaction.FindBy)
			log.Debug("Resource is assumed to be up-to-date.")
			cond := condition.Available()
			cond.Message = "Resource is assumed to be up-to-date. API call not found for FindBy."
			err = unstructuredtools.SetCondition(mg, cond)
			if err != nil {
				log.Debug("Setting condition", "error", err)
				return controller.ExternalObservation{}, err
			}

			_, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
				Pluralizer:    h.pluralizer,
				DynamicClient: h.dynamicClient,
			})

			return controller.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, err
		}
		if err != nil {
			log.Debug("Building API call", "error", err)
			return controller.ExternalObservation{}, err
		}
		reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
		if reqConfiguration == nil {
			log.Debug("Building call configuration", "error", "error building call configuration")
			return controller.ExternalObservation{}, fmt.Errorf("error building call configuration")
		}
		body, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
		if httplib.IsNotFoundError(err) {
			log.Debug("External resource not found", "kind", mg.GetKind())
			return controller.ExternalObservation{}, nil
		}
		if err != nil {
			log.Debug("Performing REST call", "error", err)
			return controller.ExternalObservation{}, err
		}
	}

	if body != nil {
		err = populateStatusFields(clientInfo, mg, body)
		if err != nil {
			log.Debug("Updating identifiers", "error", err)
			return controller.ExternalObservation{}, err
		}

		mg, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
			Pluralizer:    h.pluralizer,
			DynamicClient: h.dynamicClient,
		})
		if err != nil {
			log.Debug("Updating status", "error", err)
			return controller.ExternalObservation{}, err
		}
		ok, err := isCRUpdated(mg, *body)
		if err != nil {
			log.Debug("Checking if CR is updated", "error", err)
			return controller.ExternalObservation{}, err
		}
		if !ok {
			log.Debug("External resource not up-to-date", "kind", mg.GetKind())
			return controller.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				}, apierrors.NewNotFound(schema.GroupResource{
					Group:    mg.GroupVersionKind().Group,
					Resource: flect.Pluralize(strings.ToLower(mg.GetKind())),
				}, mg.GetName())
		}
	}
	log.Debug("Setting condition", "kind", mg.GetKind())
	err = unstructuredtools.SetCondition(mg, condition.Available())
	if err != nil {
		log.Debug("Setting condition", "error", err)
		return controller.ExternalObservation{}, err
	}
	mg, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		Pluralizer:    h.pluralizer,
		DynamicClient: h.dynamicClient,
	})
	if err != nil {
		log.Debug("Updating status", "error", err)
		return controller.ExternalObservation{}, err
	}

	log.Debug("External resource up-to-date", "kind", mg.GetKind())

	return controller.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: true,
	}, nil
}

func (h *handler) Create(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.WithValues("op", "Create").
		WithValues("apiVersion", mg.GetAPIVersion()).
		WithValues("kind", mg.GetKind()).
		WithValues("name", mg.GetName()).
		WithValues("namespace", mg.GetNamespace())

	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Debug("Getting REST client info", "error", err)
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Debug("Getting spec", "error", err)
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Create)
	if err != nil {
		log.Debug("Building API call", "error", err)
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, nil, specFields)
	body, err := apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	log.Debug("Creating external resource", "kind", mg.GetKind())

	err = unstructuredtools.SetCondition(mg, condition.Creating())
	if err != nil {
		log.Debug("Setting condition", "error", err)
		return err
	}

	err = populateStatusFields(clientInfo, mg, body)
	if err != nil {
		log.Debug("Updating identifiers", "error", err)
		return err
	}

	_, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{

		Pluralizer:    h.pluralizer,
		DynamicClient: h.dynamicClient,
	})
	if err != nil {
		log.Debug("Updating status", "error", err)
		return err
	}

	return nil
}

func (h *handler) Update(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.WithValues("op", "Update").
		WithValues("apiVersion", mg.GetAPIVersion()).
		WithValues("kind", mg.GetKind()).
		WithValues("name", mg.GetName()).
		WithValues("namespace", mg.GetNamespace())

	log.Debug("Handling custom resource values update.")
	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Debug("Getting REST client info", "error", err)
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = meta.IsVerbose(mg)

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Debug("Getting spec", "error", err)
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Update)
	if err != nil {
		log.Debug("Building API call", "error", err)
		return err
	}

	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err == fmt.Errorf("%s not found", "status") {
		log.Debug("External resource not created yet", "kind", mg.GetKind())
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	body, err := apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	err = populateStatusFields(clientInfo, mg, body)
	if err != nil {
		log.Debug("Updating identifiers", "error", err)
		return err
	}

	log.Debug("Creating external resource", "kind", mg.GetKind())

	err = unstructuredtools.SetCondition(mg, condition.Creating())
	if err != nil {
		log.Debug("Setting condition", "error", err)
		return err
	}

	mg, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		Pluralizer:    h.pluralizer,
		DynamicClient: h.dynamicClient,
	})
	if err != nil {
		log.Debug("Updating status", "error", err)
		return err
	}

	mg, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
		Pluralizer:    h.pluralizer,
		DynamicClient: h.dynamicClient,
	})
	if err != nil {
		log.Debug("Updating status", "error", err)
		return err
	}
	log.Debug("Custom resource  values updated", "kind", mg.GetKind())

	return nil

}

func (h *handler) Delete(ctx context.Context, mg *unstructured.Unstructured) error {
	log := h.logger.WithValues("op", "Delete").
		WithValues("apiVersion", mg.GetAPIVersion()).
		WithValues("kind", mg.GetKind()).
		WithValues("name", mg.GetName()).
		WithValues("namespace", mg.GetNamespace())

	log.Debug("Handling custom resource values deletion.")

	if h.swaggerInfoGetter == nil {
		return fmt.Errorf("swagger info getter must be specified")
	}

	clientInfo, err := h.swaggerInfoGetter.Get(mg)
	if err != nil {
		log.Debug("Getting REST client info", "error", err)
		return err
	}

	cli, err := restclient.BuildClient(clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Auth = clientInfo.Auth
	cli.Verbose = true

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Debug("Getting spec", "error", err)
		return err
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err != nil {
		log.Debug("Getting status", "error", err)
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Delete)
	if apiCall == nil {
		log.Debug("API call not found", "action", apiaction.Delete)
		return removeFinalizersAndUpdate(ctx, log, h.pluralizer, h.dynamicClient, mg)
	}
	if err != nil {
		log.Debug("Building API call", "error", err)
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	if reqConfiguration == nil {
		return fmt.Errorf("error building call configuration")
	}

	_, err = apiCall(ctx, http.DefaultClient, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	log.Debug("Setting condition", "kind", mg.GetKind())

	err = unstructuredtools.SetCondition(mg, condition.Deleting())
	if err != nil {
		log.Debug("Setting condition", "error", err)
		return err
	}

	return removeFinalizersAndUpdate(ctx, log, h.pluralizer, h.dynamicClient, mg)
}
