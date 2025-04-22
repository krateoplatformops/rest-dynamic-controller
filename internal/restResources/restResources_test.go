//go:build integration
// +build integration

package restResources

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gobuffalo/flect"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/restclient"

	"github.com/krateoplatformops/unstructured-runtime/pkg/controller"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	"github.com/krateoplatformops/unstructured-runtime/pkg/pluralizer"

	"github.com/krateoplatformops/snowplow/plumbing/e2e"
	xenv "github.com/krateoplatformops/snowplow/plumbing/env"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/support/kind"
)

type FakePluralizer struct {
}

var _ pluralizer.PluralizerInterface = &FakePluralizer{}

func (p FakePluralizer) GVKtoGVR(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: flect.Pluralize(strings.ToLower(gvk.Kind)),
	}, nil
}

var (
	testenv     env.Environment
	clusterName string
)

const (
	testdataPath  = "../../testdata"
	manifestsPath = "../../manifests"
	namespace     = "demo-system"
	altNamespace  = "krateo-system"
	wsUrl         = "http://localhost:30007"
)

func TestMain(m *testing.M) {
	xenv.SetTestMode(true)

	clusterName = "krateo-test"
	testenv = env.New()

	testenv.Setup(
		envfuncs.CreateClusterWithConfig(kind.NewProvider(), clusterName, filepath.Join(manifestsPath, "kind.yaml")),

		e2e.CreateNamespace(namespace),
		e2e.CreateNamespace(altNamespace),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {

			// err := decoder.ApplyWithManifestDir(ctx, cfg.Client().Resources(), manifestsPath, "ws-deployment.yaml", nil)
			// if err != nil {
			// 	return ctx, err
			// }

			// time.Sleep(30 * time.Second)

			return ctx, nil
		},
	).Finish(
	// envfuncs.DeleteNamespace(namespace),
	// envfuncs.DestroyCluster(clusterName),
	)

	os.Exit(testenv.Run(m))
}

func TestController(t *testing.T) {
	var handler controller.ExternalClient
	f := features.New("Setup").
		Setup(e2e.Logger("test")).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return ctx
		}).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r, err := resources.New(cfg.Client().RESTConfig())
			if err != nil {
				t.Error("Creating resource client.", "error", err)
				return ctx
			}

			// err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "crds"), "*.yaml", nil)
			// if err != nil {
			// 	t.Error("Applying crds manifests.", "error", err)
			// 	return ctx
			// }

			// time.Sleep(2 * time.Second)

			err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "rest"), "sample.yaml", nil, decoder.MutateNamespace(namespace))
			if err != nil {
				t.Error("Applying rest manifests.", "error", err)
				return ctx
			}

			// err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "restdefinitions", "cm"), "*.yaml", nil, decoder.MutateNamespace(namespace))
			// if err != nil {
			// 	t.Error("Applying configmap manifests.", "error", err)
			// 	return ctx
			// }

			// err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "restdefinitions"), "*.yaml", nil, decoder.MutateNamespace(namespace))
			// if err != nil {
			// 	t.Error("Applying restdefinition manifests.", "error", err)
			// 	return ctx
			// }

			zl := zap.New(zap.UseDevMode(true))
			log := logging.NewLogrLogger(zl.WithName("rest-controller-test"))

			pluralizer := &FakePluralizer{}

			var swg getter.Getter
			swg, err = getter.Dynamic(cfg.Client().RESTConfig(), pluralizer)
			if err != nil {
				log.Debug("Creating chart url info getter.", "error", err)
			}

			handler = NewHandler(cfg.Client().RESTConfig(), log, swg, pluralizer)

			return ctx
		}).Assess("Create", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		dynamic := dynamic.NewForConfigOrDie(cfg.Client().RESTConfig())
		var obj unstructured.Unstructured
		err := decoder.DecodeFile(os.DirFS(filepath.Join(testdataPath, "rest")), "sample.yaml", &obj)
		if err != nil {
			t.Error("Decoding rest manifests.", "error", err)
			return ctx
		}

		u, err := dynamic.Resource(schema.GroupVersionResource{
			Group:    "sample.krateo.io",
			Version:  "v1alpha1",
			Resource: flect.Pluralize(strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind)),
		}).Namespace(obj.GetNamespace()).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		observation, err := handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing rest resource", "error", err)
			return ctx
		}

		ctx, err = handleObservation(t, ctx, handler, observation, u)
		if err != nil {
			t.Error("Handling observation.", "error", err)
			return ctx
		}

		httpcli := http.DefaultClient
		resp, err := httpcli.Do(&http.Request{
			Method: http.MethodGet,
			URL: &url.URL{
				Scheme:   "http",
				Host:     "localhost:30007",
				RawQuery: "name=sample-1",
				Path:     "/resource",
			},
			Header: http.Header{
				"Accept": []string{"application/json"},
				"Authorization": []string{
					"Bearer " + "test",
				},
			},
		})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
		}
		defer resp.Body.Close()
		bb, _ := io.ReadAll(resp.Body)
		expected := `{"name":"sample-1","description":"Sample 1"}`
		if strings.TrimSpace(string(bb)) != strings.TrimSpace(expected) {
			t.Fatal("Response", "body", string(bb), "expected", expected)
			t.Log("Response status", "status", resp.Status)
			t.Log("Response status code", "status code", resp.StatusCode)
			t.Log("Response header", "header", resp.Header)
		}

		time.Sleep(5 * time.Second)

		return ctx
	}).Assess("Update", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		r, err := resources.New(cfg.Client().RESTConfig())
		if err != nil {
			t.Error("Creating resource client.", "error", err)
			return ctx
		}
		dy := dynamic.NewForConfigOrDie(cfg.Client().RESTConfig())
		var obj unstructured.Unstructured
		err = decoder.DecodeFile(os.DirFS(filepath.Join(testdataPath, "rest")), "sample.yaml", &obj)
		if err != nil {
			t.Error("Decoding rest manifests.", "error", err)
			return ctx
		}

		cli := dy.Resource(schema.GroupVersionResource{
			Group:    "sample.krateo.io",
			Version:  "v1alpha1",
			Resource: flect.Pluralize(strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind)),
		}).Namespace(obj.GetNamespace())

		u, err := cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		observation, err := handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing rest resource", "error", err)
			return ctx
		}
		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}
		ctx, err = handleObservation(t, ctx, handler, observation, u)
		if err != nil {
			t.Error("Handling observation.", "error", err)
			return ctx
		}

		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		err = r.Patch(ctx, u, k8s.Patch{
			PatchType: types.MergePatchType,
			Data: []byte(`{
				"spec": {
					"description": "Updated Sample Description"
				}
			}`),
		})
		if err != nil {
			t.Error("Patching Rest Resource.", "error", err)
			return ctx
		}

		time.Sleep(5 * time.Second)

		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		observation, err = handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing rest resource", "error", err)
			return ctx
		}
		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}
		ctx, err = handleObservation(t, ctx, handler, observation, u)
		if err != nil {
			t.Error("Handling observation.", "error", err)
			return ctx
		}

		httpcli := http.DefaultClient
		resp, err := httpcli.Do(&http.Request{
			Method: http.MethodGet,
			URL: &url.URL{
				Scheme:   "http",
				Host:     "localhost:30007",
				RawQuery: "name=sample-1",
				Path:     "/resource",
			},
			Header: http.Header{
				"Accept": []string{"application/json"},
				"Authorization": []string{
					"Bearer " + "test",
				},
			},
		})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
		}
		defer resp.Body.Close()
		bb, _ := io.ReadAll(resp.Body)
		expected := `{"name":"sample-1","description":"Updated Sample Description"}`
		if strings.TrimSpace(string(bb)) != strings.TrimSpace(expected) {
			t.Fatal("Response", "body", string(bb), "expected", expected)
			t.Log("Response status", "status", resp.Status)
			t.Log("Response status code", "status code", resp.StatusCode)
			t.Log("Response header", "header", resp.Header)
		}

		return ctx
	}).Assess("Delete", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		r, err := resources.New(cfg.Client().RESTConfig())
		if err != nil {
			t.Error("Creating resource client.", "error", err)
			return ctx
		}
		dy := dynamic.NewForConfigOrDie(cfg.Client().RESTConfig())
		var obj unstructured.Unstructured
		err = decoder.DecodeFile(os.DirFS(filepath.Join(testdataPath, "rest")), "sample.yaml", &obj)
		if err != nil {
			t.Error("Decoding rest manifests.", "error", err)
			return ctx
		}

		cli := dy.Resource(schema.GroupVersionResource{
			Group:    "sample.krateo.io",
			Version:  "v1alpha1",
			Resource: flect.Pluralize(strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind)),
		}).Namespace(obj.GetNamespace())

		u, err := cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		observation, err := handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing rest resource", "error", err)
			return ctx
		}
		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}
		ctx, err = handleObservation(t, ctx, handler, observation, u)
		if err != nil {
			t.Error("Handling observation.", "error", err)
			return ctx
		}

		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		u.SetFinalizers([]string{
			"composition.krateo.io/finalizer",
		})

		u, err = cli.Update(ctx, u, metav1.UpdateOptions{})
		if err != nil {
			t.Error("Updating composition.", "error", err)
			return ctx
		}

		err = r.Delete(ctx, u)
		if err != nil {
			t.Error("Deleting Rest Resource.", "error", err)
			return ctx
		}

		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}

		observation, err = handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing rest resource", "error", err)
			return ctx
		}
		u, err = cli.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
			return ctx
		}
		ctx, err = handleObservation(t, ctx, handler, observation, u)
		if err != nil {
			t.Error("Handling observation.", "error", err)
			return ctx
		}

		u.SetFinalizers([]string{})
		u, err = cli.Update(ctx, u, metav1.UpdateOptions{})
		if err != nil {
			t.Error("Updating composition.", "error", err)
			return ctx
		}

		httpcli := http.DefaultClient
		resp, err := httpcli.Do(&http.Request{
			Method: http.MethodGet,
			URL: &url.URL{
				Scheme:   "http",
				Host:     "localhost:30007",
				RawQuery: "name=sample-1",
				Path:     "/resource",
			},
			Header: http.Header{
				"Accept": []string{"application/json"},
				"Authorization": []string{
					"Bearer " + "test",
				},
			},
		})
		if err != nil {
			t.Error("Getting Rest Resource.", "error", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Log("Response status", "status", resp.Status)
			t.Log("Response status code", "status code", resp.StatusCode)
			t.Log("Response header", "header", resp.Header)
		}

		return ctx
	}).Feature()

	testenv.Test(t, f)
}

func handleObservation(t *testing.T, ctx context.Context, handler controller.ExternalClient, observation controller.ExternalObservation, u *unstructured.Unstructured) (context.Context, error) {
	var err error
	if observation.ResourceExists == true && observation.ResourceUpToDate == true {
		observation, err = handler.Observe(ctx, u)
		if err != nil {
			t.Error("Observing composition.", "error", err)
			return ctx, err
		}
		if observation.ResourceExists == true && observation.ResourceUpToDate == true {
			t.Log("Composition already exists and is ready.")
			return ctx, nil
		}
	} else if observation.ResourceExists == false && observation.ResourceUpToDate == true {
		err = handler.Delete(ctx, u)
		if err != nil {
			t.Error("Deleting composition.", "error", err)
			return ctx, err
		}
	} else if observation.ResourceExists == true && observation.ResourceUpToDate == false {
		err = handler.Update(ctx, u)
		if err != nil {
			t.Error("Updating composition.", "error", err)
			return ctx, err
		}
	} else if observation.ResourceExists == false && observation.ResourceUpToDate == false {
		err = handler.Create(ctx, u)
		if err != nil {
			t.Error("Creating composition.", "error", err)
			return ctx, err
		}
	}
	return ctx, nil
}
