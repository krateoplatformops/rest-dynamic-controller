package getter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gobuffalo/flect"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/client/restclient"
	"github.com/krateoplatformops/rest-dynamic-controller/internal/text"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"github.com/lucasepe/httplib"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type VerbsDescription struct {
	// Name of the action to perform when this api is called
	Action string `json:"action"`
	// Method: the http method to use [GET, POST, PUT, DELETE, PATCH]
	Method string `json:"method"`
	// Path: the path to the api
	Path string `json:"path"`
	// // AltFieldMapping: the alternative mapping of the fields to use in the request
	// AltFieldMapping map[string]string `json:"altFieldMapping,omitempty"`
}

type Resource struct {
	// Name: the name of the resource to manage
	Kind string `json:"kind"`
	// Identifiers: the list of fields to use as identifiers
	Identifiers []string `json:"identifiers"`
	// VerbsDescription: the list of verbs to use on this resource
	VerbsDescription []VerbsDescription `json:"verbsDescription"`
}

type GVK struct {
	// Group: the group of the resource
	// +optional
	Group string `json:"group,omitempty"`
	// Version: the version of the resource
	// +optional
	Version string `json:"version,omitempty"`
	// Kind: the kind of the resource
	// +optional
	Kind string `json:"kind,omitempty"`
}

type ReferenceInfo struct {
	// Field: the field to use as reference - represents the id of the resource
	// +optional
	Field string `json:"field,omitempty"`

	// GVK: the group, version, kind of the resource
	// +optional
	GroupVersionKind GVK `json:"groupVersionKind,omitempty"`
}

type Info struct {
	// URL of the OAS 3.0 JSON file that is being requested.
	URL string `json:"url"`

	// The resource to manage
	Resource Resource `json:"resources,omitempty"`

	// The authentication method to use
	Auth httplib.AuthMethod `json:"auth,omitempty"`

	// Verbose: if true, the client will dump verbose output
	Verbose bool `json:"verbose,omitempty"`
}

type Getter interface {
	Get(un *unstructured.Unstructured) (*Info, error)
}

func Static(chart string) Getter {
	return staticGetter{chartName: chart}
}

func Dynamic(cfg *rest.Config) (Getter, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &dynamicGetter{
		dynamicClient: dyn,
	}, nil
}

var _ Getter = (*staticGetter)(nil)

type staticGetter struct {
	chartName string
}

func (pig staticGetter) Get(_ *unstructured.Unstructured) (*Info, error) {
	return &Info{
		URL: pig.chartName,
	}, nil
}

var _ Getter = (*dynamicGetter)(nil)

type dynamicGetter struct {
	dynamicClient dynamic.Interface
}

func (g *dynamicGetter) Get(un *unstructured.Unstructured) (*Info, error) {
	gvr, err := unstructuredtools.GVR(un)
	if err != nil {
		return nil, err
	}

	// sel, err := selectorForGroup(gvr)
	// if err != nil {
	// 	return nil, err
	// }

	gvrForDefinitions := schema.GroupVersionResource{
		Group:    "swaggergen.krateo.io",
		Version:  "v1alpha1",
		Resource: "restdefinitions",
	}

	all, err := g.dynamicClient.Resource(gvrForDefinitions).
		Namespace(un.GetNamespace()).
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(all.Items) == 0 {
		return nil, fmt.Errorf("no definitions found for '%v' in namespace: %s", gvr, un.GetNamespace())
	}

	for _, item := range all.Items {
		res, ok, err := unstructured.NestedFieldNoCopy(item.Object, "spec", "resource")
		if !ok {
			return nil, fmt.Errorf("missing spec.resources in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		if err != nil {
			return nil, err
		}

		group, ok, err := unstructured.NestedString(item.Object, "spec", "resourceGroup")
		if !ok {
			return nil, fmt.Errorf("missing spec.resourceGroup in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		if err != nil {
			return nil, err
		}

		kind, ok, err := unstructured.NestedString(item.Object, "spec", "resource", "kind")
		if !ok {
			return nil, fmt.Errorf("missing kind in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		if err != nil {
			return nil, err
		}
		if kind != un.GetKind() {
			continue
		}

		oasPath, ok, err := unstructured.NestedString(item.Object, "spec", "oasPath")
		if !ok {
			return nil, fmt.Errorf("missing spec.oasPath in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		if err != nil {
			return nil, err
		}

		if group == gvr.Group {
			gvk := un.GroupVersionKind()
			// Convert the map to JSON
			jsonData, err := json.Marshal(res)
			if err != nil {
				return nil, err
			}
			// Convert the JSON to a struct
			var resource Resource
			err = json.Unmarshal(jsonData, &resource)
			if err != nil {
				return nil, err
			}

			auth, err := g.getAuth(un)
			if err != nil {
				return nil, err
			}

			if resource.Kind == gvk.Kind {
				return &Info{
					URL:      oasPath,
					Resource: resource,
					Auth:     auth,
				}, nil
			}
		}
	}
	return nil, nil
}

// getAuth returns the authentication method for the given resource.
// It returns an error if the authentication object is not valid.
func (g *dynamicGetter) getAuth(un *unstructured.Unstructured) (httplib.AuthMethod, error) {
	gvr, err := unstructuredtools.GVR(un)
	if err != nil {
		return nil, err
	}

	var authRef string
	var authType restclient.AuthType = restclient.AuthTypeBasic

	authenticationRefsMap, ok, err := unstructured.NestedStringMap(un.Object, "spec", "authenticationRefs")
	if err != nil {
		return nil, fmt.Errorf("error getting spec.authenticationRefs for '%v' in namespace: %s", gvr, un.GetNamespace())
	}
	if !ok {
		return nil, nil
	}

	for key := range authenticationRefsMap {
		authRef, ok, err = unstructured.NestedString(un.Object, "spec", "authenticationRefs", key)
		if err != nil {
			return nil, fmt.Errorf("error getting spec.authenticationRefs.%s for '%v' in namespace: %s", key, gvr, un.GetNamespace())
		}
		if ok {
			authType, err = restclient.ToType(strings.Split(key, "AuthRef")[0])
			if err != nil {
				return nil, err
			}
			break
		}
	}

	// authRef, ok, err = unstructured.NestedString(un.Object, "spec", "basicAuthRef")
	// if err != nil {
	// 	return nil, fmt.Errorf()
	// }
	// if !ok {
	// 	authRef, ok, err = unstructured.NestedString(un.Object, "spec", "bearerAuthRef")
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	if !ok {
	// 		return nil, fmt.Errorf("missing spec.basicAuthRef or spec.bearerAuthRef in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
	// 	}
	// 	authType = restclient.AuthTypeBearer
	// }
	gvrForAuthentication := schema.GroupVersionResource{
		Group:    gvr.Group,
		Version:  "v1alpha1",
		Resource: strings.ToLower(flect.Pluralize(fmt.Sprintf("%sAuth", text.ToGolangName(authType.String())))),
	}

	auth, err := g.dynamicClient.Resource(gvrForAuthentication).
		Namespace(un.GetNamespace()).
		Get(context.Background(), authRef, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return parseAuthentication(auth, authType, g.dynamicClient)
}

// parseAuthentication parses the authentication object and returns the appropriate AuthMethod for the given AuthType.
// It returns an error if the authentication object is not valid.
func parseAuthentication(un *unstructured.Unstructured, authType restclient.AuthType, dyn dynamic.Interface) (httplib.AuthMethod, error) {
	gvr, err := unstructuredtools.GVR(un)
	if err != nil {
		return nil, err
	}
	if authType == restclient.AuthTypeBasic {
		username, ok, err := unstructured.NestedString(un.Object, "spec", "username")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("missing spec.username in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		passwordRef, ok, err := unstructured.NestedStringMap(un.Object, "spec", "passwordRef")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("missing spec.passwordRef in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}

		password, err := GetSecret(context.Background(), dyn, SecretKeySelector{
			Name:      passwordRef["name"],
			Namespace: passwordRef["namespace"],
			Key:       passwordRef["key"],
		})
		if err != nil {
			return nil, fmt.Errorf("error getting password for '%v' in namespace: %s - %w", gvr, un.GetNamespace(), err)
		}

		return &httplib.BasicAuth{
			Username: username,
			Password: password,
		}, nil
	} else if authType == restclient.AuthTypeBearer {
		tokenRef, ok, err := unstructured.NestedStringMap(un.Object, "spec", "tokenRef")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("missing spec.tokenRef in definition for '%v' in namespace: %s", gvr, un.GetNamespace())
		}
		token, err := GetSecret(context.Background(), dyn, SecretKeySelector{
			Name:      tokenRef["name"],
			Namespace: tokenRef["namespace"],
			Key:       tokenRef["key"],
		})
		if err != nil {
			return nil, fmt.Errorf("error getting token for '%v' in namespace: %s - %w", gvr, un.GetNamespace(), err)
		}

		return &httplib.TokenAuth{
			Token: token,
		}, nil
	}
	return nil, fmt.Errorf("unknown auth type: %s", authType)
}

type SecretKeySelector struct {
	Name      string
	Namespace string
	Key       string
}

func GetSecret(ctx context.Context, client dynamic.Interface, secretKeySelector SecretKeySelector) (string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}

	sec, err := client.Resource(gvr).Namespace(secretKeySelector.Namespace).Get(ctx, secretKeySelector.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	data, _, err := unstructured.NestedMap(sec.Object, "data")
	if err != nil {
		return "", err
	}
	bsec := data[secretKeySelector.Key].(string)
	bkey, err := base64.StdEncoding.DecodeString(bsec)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret key: %w", err)
	}
	return string(bkey), nil
}
