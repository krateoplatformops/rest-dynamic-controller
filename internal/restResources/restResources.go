package restResources

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/client"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/tools/apiaction"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/restclient"
	"github.com/krateoplatformops/unstructured-runtime/pkg/controller"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	"github.com/krateoplatformops/unstructured-runtime/pkg/meta"
	"github.com/krateoplatformops/unstructured-runtime/pkg/pluralizer"
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"

	"github.com/krateoplatformops/unstructured-runtime/pkg/tools"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var _ controller.ExternalClient = (*handler)(nil)

var (
	ErrStatusNotFound = errors.New("status not found")
)

func NewHandler(cfg *rest.Config, log logging.Logger, swg getter.Getter, pluralizer pluralizer.PluralizerInterface) controller.ExternalClient {
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
	pluralizer        pluralizer.PluralizerInterface
	logger            logging.Logger
	dynamicClient     dynamic.Interface
	discoveryClient   *discovery.DiscoveryClient
	swaggerInfoGetter getter.Getter
}

func (h *handler) Observe(ctx context.Context, mg *unstructured.Unstructured) (controller.ExternalObservation, error) {
	if mg == nil {
		return controller.ExternalObservation{}, fmt.Errorf("custom resource is nil")
	}

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

	cli, err := restclient.BuildClient(ctx, h.dynamicClient, clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return controller.ExternalObservation{}, err
	}

	cli.Debug = meta.IsVerbose(mg)
	cli.SetAuth = clientInfo.SetAuth
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

	var body any
	isKnown := isResourceKnown(cli, log, clientInfo, statusFields, specFields)
	if isKnown {
		// Getting the external resource by its identifier
		apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Get)
		if apiCall == nil || callInfo == nil {
			log.Info("API action get not found", "action", apiaction.Update)
			return controller.ExternalObservation{}, fmt.Errorf("API action get not found for %s", apiaction.Get)
		}
		if err != nil {
			log.Debug("Building API call", "error", err)
			return controller.ExternalObservation{}, err
		}
		reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
		if reqConfiguration == nil {
			return controller.ExternalObservation{}, fmt.Errorf("error building call configuration")
		}
		body, err = apiCall(ctx, &http.Client{}, callInfo.Path, reqConfiguration)
		if restclient.IsNotFoundError(err) {
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
			err = unstructuredtools.SetConditions(mg, cond)
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
		body, err = apiCall(ctx, &http.Client{}, callInfo.Path, reqConfiguration)
		if restclient.IsNotFoundError(err) {
			log.Debug("External resource not found", "kind", mg.GetKind())
			return controller.ExternalObservation{}, nil
		}
		if err != nil {
			log.Debug("Performing REST call", "error", err)
			return controller.ExternalObservation{}, err
		}
	}

	// Body can be nil if the API does not return anything on get with a proper status code (204 No Content, 304 Not Modified).
	if body == nil {
		cond := condition.Available()
		cond.Message = "Resource is assumed to be up-to-date. Returned body is nil."
		err = unstructuredtools.SetConditions(mg, cond)
		if err != nil {
			log.Debug("Setting condition", "error", err)
			return controller.ExternalObservation{}, err
		}
		_, err = tools.UpdateStatus(ctx, mg, tools.UpdateOptions{
			Pluralizer:    h.pluralizer,
			DynamicClient: h.dynamicClient,
		})
		if err != nil {
			log.Debug("Updating status", "error", err)
			return controller.ExternalObservation{}, err
		}
		log.Debug("Resource is assumed to be up-to-date. Returned body is nil.")
		return controller.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	b, ok := body.(*map[string]interface{})
	if !ok {
		log.Debug("Performing REST call", "error", "body is not an object")
		return controller.ExternalObservation{}, fmt.Errorf("body is not an object")
	}
	if b != nil {
		err = populateStatusFields(clientInfo, mg, b)
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
		res, err := isCRUpdated(mg, *b)
		if err != nil {
			log.Debug("Checking if CR is updated", "error", err)
			return controller.ExternalObservation{}, err
		}
		if !res.IsEqual {
			cond := condition.Unavailable()
			if res.Reason != nil {
				cond.Reason = fmt.Sprintf("Resource is not up-to-date due to %s - spec value: %s, remote value: %s", res.Reason.Reason, res.Reason.FirstValue, res.Reason.SecondValue)
			}

			unstructuredtools.SetConditions(mg, cond)
			log.Debug("External resource not up-to-date", "kind", mg.GetKind())
			return controller.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: false,
			}, nil
		}
	}
	log.Debug("Setting condition", "kind", mg.GetKind())
	err = unstructuredtools.SetConditions(mg, condition.Available())
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

	cli, err := restclient.BuildClient(ctx, h.dynamicClient, clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Debug = meta.IsVerbose(mg)
	cli.SetAuth = clientInfo.SetAuth

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
	if apiCall == nil || callInfo == nil {
		log.Info("API action create not found", "action", apiaction.Update)
		return nil
	}
	reqConfiguration := BuildCallConfig(callInfo, nil, specFields)
	body, err := apiCall(ctx, &http.Client{}, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	if body != nil {
		b, ok := body.(*map[string]interface{})
		if !ok {
			log.Debug("Performing REST call", "error", "body is not an object")
			return fmt.Errorf("body is not an object")
		}

		err = populateStatusFields(clientInfo, mg, b)
		if err != nil {
			log.Debug("Updating identifiers", "error", err)
			return err
		}
	}

	log.Debug("Creating external resource", "kind", mg.GetKind())

	err = unstructuredtools.SetConditions(mg, condition.Creating())
	if err != nil {
		log.Debug("Setting condition", "error", err)
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

	cli, err := restclient.BuildClient(ctx, h.dynamicClient, clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Debug = meta.IsVerbose(mg)
	cli.SetAuth = clientInfo.SetAuth

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
	if apiCall == nil || callInfo == nil {
		log.Info("API action update not found", "action", apiaction.Update)
		return nil
	}

	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err == ErrStatusNotFound {
		log.Debug("External resource not created yet", "kind", mg.GetKind())
		return err
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	body, err := apiCall(ctx, &http.Client{}, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	// Body can be empty if the API does not return anything on update with a proper status code (204 No Content, 304 Not Modified).
	if body != nil {
		b, ok := body.(*map[string]interface{})
		if !ok {
			log.Debug("Performing REST call", "error", "body is not an object")
			return fmt.Errorf("body is not an object")
		}

		err = populateStatusFields(clientInfo, mg, b)
		if err != nil {
			log.Debug("Updating identifiers", "error", err)
			return err
		}
	}

	log.Debug("Updating external resource", "kind", mg.GetKind())

	err = unstructuredtools.SetConditions(mg, condition.Creating())
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

	log.Debug("Custom resource values updated", "kind", mg.GetKind())

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

	cli, err := restclient.BuildClient(ctx, h.dynamicClient, clientInfo.URL)
	if err != nil {
		log.Debug("Building REST client", "error", err)
		return err
	}
	cli.Debug = meta.IsVerbose(mg)
	cli.SetAuth = clientInfo.SetAuth

	specFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "spec")
	if err != nil {
		log.Debug("Getting spec", "error", err)
		return err
	}
	statusFields, err := unstructuredtools.GetFieldsFromUnstructured(mg, "status")
	if err == ErrStatusNotFound {
		log.Debug("External resource not created yet", "kind", mg.GetKind())
		log.Debug("Remote resource is assumed to not exist, deleting CR")
		err = unstructuredtools.SetConditions(mg, condition.Deleting())
		if err != nil {
			log.Debug("Setting condition", "error", err)
		}
		return nil
	}
	if err != nil {
		log.Debug("Getting status", "error", err)
		return err
	}
	apiCall, callInfo, err := APICallBuilder(cli, clientInfo, apiaction.Delete)
	if err != nil {
		log.Debug("Building API call", "error", err)
		return err
	}
	if apiCall == nil || callInfo == nil {
		log.Info("API action delete not found", "action", apiaction.Update)
		return nil
	}
	reqConfiguration := BuildCallConfig(callInfo, statusFields, specFields)
	if reqConfiguration == nil {
		return fmt.Errorf("error building call configuration")
	}

	_, err = apiCall(ctx, &http.Client{}, callInfo.Path, reqConfiguration)
	if err != nil {
		log.Debug("Performing REST call", "error", err)
		return err
	}

	log.Debug("Setting condition", "kind", mg.GetKind())

	err = unstructuredtools.SetConditions(mg, condition.Deleting())
	if err != nil {
		log.Debug("Setting condition", "error", err)
		return err
	}

	return nil
}
