//go:build integration
// +build integration

package restResources

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/client-go/dynamic"

	"github.com/gobuffalo/flect"
	customcondition "github.com/krateoplatformops/rest-dynamic-controller/internal/controllers/condition"
	getter "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/definitiongetter"

	"github.com/krateoplatformops/unstructured-runtime/pkg/controller"
	"github.com/krateoplatformops/unstructured-runtime/pkg/logging"
	"github.com/krateoplatformops/unstructured-runtime/pkg/pluralizer"
	unstructuredtools "github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured"
	"github.com/krateoplatformops/unstructured-runtime/pkg/tools/unstructured/condition"

	"github.com/krateoplatformops/snowplow/plumbing/e2e"
	xenv "github.com/krateoplatformops/snowplow/plumbing/env"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/e2e-framework/klient/decoder"
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
	testenv       env.Environment
	clusterName   string
	mockServerCmd *exec.Cmd
)

const (
	testdataPath  = "../../testdata"
	manifestsPath = "../../manifests"
	namespace     = "default"
	altNamespace  = "demo-system"
	wsUrl         = "http://localhost:30007"
)

func killProcessOnPort(port int) {
	// Get processes using the port
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		return
	}

	pids := strings.Fields(strings.TrimSpace(string(output)))
	for _, pid := range pids {
		// Check what process this actually is
		checkCmd := exec.Command("ps", "-p", pid, "-o", "comm=,command=")
		psOutput, err := checkCmd.Output()
		if err != nil {
			continue
		}

		cmdLine := string(psOutput)

		// Only kill if it's our mock server (not Docker or other services)
		if strings.Contains(cmdLine, "main") {
			exec.Command("kill", "-9", pid).Run()
		}
	}
}

func startMockServer() error {
	// Kill eventuali processi rimasti dalla volta precedente
	killProcessOnPort(30007)

	// Aspetta un attimo per essere sicuri
	time.Sleep(500 * time.Millisecond)

	// Avvia il mock server come processo separato
	mockServerCmd = exec.Command("go", "run", "internal/controllers/mockserver/main.go")
	// Redirect output per debug in un file di log
	logFile, err := os.Create("mockserver.log")
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	mockServerCmd.Stdout = logFile
	mockServerCmd.Stderr = logFile
	mockServerCmd.Dir = "../.." // Vai alla root del progetto

	if err := mockServerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start mock server: %w", err)
	}

	// Aspetta che il server sia pronto
	for i := 0; i < 30; i++ {
		resp, err := http.Get("http://localhost:30007/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(time.Second)
	}

	// Se arriviamo qui, killa il processo e restituisci errore
	stopMockServer()
	return fmt.Errorf("mock server failed to start within 30 seconds")
}

func stopMockServer() {
	if mockServerCmd != nil && mockServerCmd.Process != nil {
		// Prima prova SIGTERM gentile
		mockServerCmd.Process.Signal(syscall.SIGTERM)

		// Aspetta un po'
		done := make(chan error, 1)
		go func() {
			done <- mockServerCmd.Wait()
		}()

		select {
		case <-done:
			// Processo terminato correttamente
		case <-time.After(2 * time.Second):
			// Timeout, forza il kill
			mockServerCmd.Process.Kill()
			mockServerCmd.Wait()
		}

		mockServerCmd = nil
	}

	// Cleanup finale per essere sicuri
	killProcessOnPort(30007)
}

func TestMain(m *testing.M) {
	xenv.SetTestMode(true)

	clusterName = "krateo"
	testenv = env.New()

	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), clusterName),
		e2e.CreateNamespace(namespace),
		e2e.CreateNamespace(altNamespace),

		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			if err := startMockServer(); err != nil {
				return ctx, err
			}

			time.Sleep(2 * time.Second)

			return ctx, nil
		},
	).Finish(
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			stopMockServer()
			return ctx, nil
		},
		envfuncs.DestroyCluster(clusterName),
	)

	os.Exit(testenv.Run(m))
}

func TestController(t *testing.T) {
	var handler controller.ExternalClient
	var cli dynamic.Interface
	f := features.New("Setup").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r, err := resources.New(cfg.Client().RESTConfig())
			if err != nil {
				t.Error("Creating resource client.", "error", err)
				return ctx
			}

			cli = dynamic.NewForConfigOrDie(cfg.Client().RESTConfig())

			err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "crds"), "*.yaml", nil)
			if err != nil {
				t.Error("Applying crds manifests.", "error", err)
				return ctx
			}

			time.Sleep(2 * time.Second)

			err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "rest"), "*.yaml", nil)
			if err != nil {
				t.Error("Applying rest manifests.", "error", err)
				return ctx
			}

			// Read OAS from the cm folder and put in a ConfigMap
			b, err := os.ReadFile(filepath.Join(testdataPath, "restdefinitions", "cm", "oas.yaml"))
			if err != nil {
				t.Error("Reading OAS file.", "error", err)
				return ctx
			}

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sample",
					Namespace: altNamespace,
				},
				Data: map[string]string{
					"openapi.yaml": string(b),
				},
			}
			err = r.Create(ctx, &cm)
			if err != nil {
				t.Error("Creating ConfigMap with OAS.", "error", err)
				return ctx
			}

			err = decoder.ApplyWithManifestDir(ctx, r, filepath.Join(testdataPath, "restdefinitions"), "*.yaml", nil)
			if err != nil {
				t.Error("Applying restdefinition manifests.", "error", err)
				return ctx
			}

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
		}).
		// Test operazione Create
		Assess("Create", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			configPayload := `{"authFailures": false}`
			_, err := http.Post("http://localhost:30007/admin/config", "application/json",
				strings.NewReader(configPayload))
			if err != nil {
				t.Error("Resetting auth config", "error", err)
				return ctx
			}
			resourceName := "sample-1"
			k := cli.Resource(schema.GroupVersionResource{
				Group:    "sample.krateo.io",
				Version:  "v1alpha1",
				Resource: "samples",
			}).Namespace(namespace)

			u, err := k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Getting Rest Resource.", "error", err)
				return ctx
			}

			obs, err := handler.Observe(ctx, u)
			if err != nil {
				t.Error("Error during initial observe", "error", err)
				return ctx
			}

			if obs.ResourceExists {
				t.Error("Resource should not exist initially")
				return ctx
			}

			u, err = k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource for create test", "error", err)
				return ctx
			}
			// Crea la risorsa
			err = handler.Create(ctx, u)
			if err != nil {
				t.Error("Error creating resource", "error", err)
				return ctx
			}

			u, err = k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource after create", "error", err)
				return ctx
			}

			// Verifica che la risorsa sia stata creata
			time.Sleep(1 * time.Second) // Piccola pausa per la propagazione
			obs, err = handler.Observe(ctx, u)
			if err != nil {
				t.Error("Error observing after create", "error", err)
				return ctx
			}

			if !obs.ResourceExists {
				t.Error("Resource should exist after creation")
				return ctx
			}

			if obs.ResourceUpToDate {
				t.Log("Resource is up to date after creation")
			}

			return ctx
		}).
		Assess("AsyncCreate", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			var resourceNames []string
			resourceNames = append(resourceNames, "sample-async-1", "sample-async-get")

			// Configure the mock server to simulate async creation
			configPayload := `{"asyncOperations": true}`
			_, err := http.Post("http://localhost:30007/admin/config", "application/json",
				strings.NewReader(configPayload))
			if err != nil {
				t.Error("Configuring mock server for async create", "error", err)
				return ctx
			}

			for _, resourceName := range resourceNames {
				k := cli.Resource(schema.GroupVersionResource{
					Group:    "sample.krateo.io",
					Version:  "v1alpha1",
					Resource: "samples",
				}).Namespace(namespace)

				u, err := k.Get(ctx, resourceName, metav1.GetOptions{})
				if err != nil {
					t.Error("Getting Rest Resource.", "error", err)
					return ctx
				}

				obs, err := handler.Observe(ctx, u)
				if err != nil {
					t.Error("Error during initial observe", "error", err)
					return ctx
				}

				if obs.ResourceExists {
					t.Error("Resource should not exist initially")
					return ctx
				}

				// Trigger async create
				err = handler.Create(ctx, u)
				if err != nil {
					t.Error("Error creating resource", "error", err)
					return ctx
				}

				// Poll for resource existence (simulate async propagation)
				var found bool
				for i := 0; i < 10; i++ {
					if i == 5 {
						configPayload := `{"completePendingAsync": true}`
						_, err := http.Post("http://localhost:30007/admin/config", "application/json",
							strings.NewReader(configPayload))
						if err != nil {
							t.Error("Configuring mock server for async create", "error", err)
							return ctx
						}
					}

					time.Sleep(1 * time.Second)
					u, err = k.Get(ctx, resourceName, metav1.GetOptions{})
					if err != nil {
						t.Error("Error getting resource after async create", "error", err)
						return ctx
					}
					obs, err = handler.Observe(ctx, u)

					u, err = k.Get(ctx, resourceName, metav1.GetOptions{})
					if err != nil {
						t.Error("Error getting resource after async create", "error", err)
						return ctx
					}
					if unstructuredtools.IsConditionSet(u, customcondition.Pending()) {
						t.Log("Resource is still pending, waiting for async creation to complete")
						continue
					} else if unstructuredtools.IsConditionSet(u, condition.Available()) {
						found = true
						t.Log("Resource is now available after async creation")
						break
					}

				}
				if !found {
					t.Error("Resource was not created asynchronously within timeout")
					return ctx
				}

				if !obs.ResourceUpToDate {
					t.Error("Resource should be up to date after async creation")
				}
			}

			// Reset mock server config if needed
			configPayload = `{"asyncOperations": false}`
			http.Post("http://localhost:30007/admin/config", "application/json", strings.NewReader(configPayload))

			return ctx
		}).
		Assess("AlreadyExists", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			resourceName := "sample-1-already-exists"
			k := cli.Resource(schema.GroupVersionResource{
				Group:    "sample.krateo.io",
				Version:  "v1alpha1",
				Resource: "samples",
			}).Namespace(namespace)

			u, err := k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource for already exists test", "error", err)
				return ctx
			}

			obs, err := handler.Observe(ctx, u)
			if err != nil {
				t.Error("Error during initial observe", "error", err)
				return ctx
			}

			if !obs.ResourceExists {
				t.Error("Resource should exist initially")
				return ctx
			}

			if !obs.ResourceUpToDate {
				t.Error("Resource should be up to date initially")
				return ctx
			}
			ok, err := unstructuredtools.IsAvailable(u)
			if err != nil {
				t.Error("Error checking if resource is available", "error", err)
			}
			if !ok {
				t.Error("Resource should be available initially")
			}

			return ctx
		}).
		Assess("Update", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			resourceName := "sample-1"
			k := cli.Resource(schema.GroupVersionResource{
				Group:    "sample.krateo.io",
				Version:  "v1alpha1",
				Resource: "samples",
			}).Namespace(namespace)
			u, err := k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource for update test", "error", err)
				return ctx
			}

			// Edit the resource
			u.Object["spec"].(map[string]interface{})["description"] = "Updated description"
			// Apply the update on the cluster
			u, err = k.Update(ctx, u, metav1.UpdateOptions{})
			if err != nil {
				t.Error("Error applying update to resource", "error", err)
				return ctx
			}

			// Observe the resource. We expect it to exist but not be up to date
			obs, err := handler.Observe(ctx, u)
			if err != nil {
				t.Error("Error observing resource after update", "error", err)
				return ctx
			}
			if !obs.ResourceExists {
				t.Error("Resource should exist after update")
				return ctx
			}
			if obs.ResourceUpToDate {
				t.Error("Resource should not be up to date after edit")
				return ctx
			}

			err = handler.Update(ctx, u)
			if err != nil {
				t.Error("Error updating resource", "error", err)
				return ctx
			}

			u, err = k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource after update", "error", err)
				return ctx
			}

			time.Sleep(1 * time.Second)
			obs, err = handler.Observe(ctx, u)
			if err != nil {
				t.Error("Error observing after update", "error", err)
				return ctx
			}

			if !obs.ResourceExists {
				t.Error("Resource should exist after update")
				return ctx
			}
			if !obs.ResourceUpToDate {
				t.Error("Resource should be up to date after update")
			}
			t.Log("Resource is up to date after update")

			return ctx
		}).
		// Test operazione Delete
		Assess("Delete", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			resourceName := "sample-1"
			k := cli.Resource(schema.GroupVersionResource{
				Group:    "sample.krateo.io",
				Version:  "v1alpha1",
				Resource: "samples",
			}).Namespace(namespace)

			u, err := k.Get(ctx, resourceName, metav1.GetOptions{})
			if err != nil {
				t.Error("Error getting resource for delete test", "error", err)
				return ctx
			}

			// Delete the resource
			err = handler.Delete(ctx, u)
			if err != nil {
				t.Error("Error deleting resource", "error", err)
			}

			// curl to check if the resource is deleted
			req, err := http.NewRequest("GET", "http://localhost:30007/resource/sample-1", nil)
			if err != nil {
				t.Error("Error creating HTTP request for resource deletion check", "error", err)
			} else {
				req.Header.Set("Authorization", "Bearer test")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error("Error checking resource deletion via HTTP", "error", err)
				}

				if resp.StatusCode != http.StatusNotFound {
					t.Error("Resource should be deleted, expected 404 but got", resp.StatusCode)
				}
				if resp.StatusCode != http.StatusNotFound {
					t.Error("Resource should be deleted, expected 404 but got", resp.StatusCode)
				}
			}

			return ctx
		}).Feature()

	testenv.Test(t, f)
}

// Test per la configurazione del mock server
func TestMockServerConfiguration(t *testing.T) {
	// Verifica che il mock server risponda correttamente
	resp, err := http.Get("http://localhost:30007/health")
	if err != nil {
		t.Skip("Mock server not available, skipping configuration test")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Error("Mock server health check failed", "status", resp.StatusCode)
	}

	// Test configurazione errori
	configPayload := `{"simulateErrors": true}`
	resp, err = http.Post("http://localhost:30007/admin/config", "application/json",
		strings.NewReader(configPayload))
	if err != nil {
		t.Error("Failed to configure mock server", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Error("Mock server configuration failed", "status", resp.StatusCode)
	}

	// Ripristina configurazione normale
	configPayload = `{"simulateErrors": false}`
	resp, err = http.Post("http://localhost:30007/admin/config", "application/json",
		strings.NewReader(configPayload))
	if err != nil {
		t.Error("Failed to reset mock server config", "error", err)
		return
	}
	defer resp.Body.Close()
}
