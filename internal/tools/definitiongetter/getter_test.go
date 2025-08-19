package getter

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

type mockPluralizerInterface struct {
	mock.Mock
}

func (m *mockPluralizerInterface) GVKtoGVR(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	args := m.Called(gvk)
	return args.Get(0).(schema.GroupVersionResource), args.Error(1)
}

func TestDynamic(t *testing.T) {
	t.Run("nil config should return error", func(t *testing.T) {
		mockPlural := &mockPluralizerInterface{}
		_, err := Dynamic(nil, mockPlural)
		assert.Error(t, err)
	})
}

func TestDynamicGetter_Get(t *testing.T) {
	tests := []struct {
		name           string
		unstructured   *unstructured.Unstructured
		definitions    []runtime.Object
		configs        []runtime.Object
		secrets        []runtime.Object
		setupMocks     func(*mockPluralizerInterface)
		wantErr        bool
		wantErrMessage string
		validateResult func(*testing.T, *Info)
	}{
		{
			name: "successful retrieval with no authentication configured",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec": map[string]interface{}{
						"resourceGroup": "test.io",
						"oasPath":       "/api/v1/oas.yaml",
						"resource":      map[string]interface{}{"kind": "TestResource"},
					},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
			},
			wantErr: false,
			validateResult: func(t *testing.T, info *Info) {
				assert.NotNil(t, info)
				assert.Equal(t, "/api/v1/oas.yaml", info.URL)
				assert.Nil(t, info.SetAuth, "SetAuth should be nil when no auth is configured")
			},
		},
		{
			name: "successful retrieval with bearer authentication",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": map[string]interface{}{"name": "test-config"}},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec": map[string]interface{}{
						"resourceGroup": "test.io",
						"oasPath":       "/api/v1/oas.yaml",
						"resource":      map[string]interface{}{"kind": "TestResource"},
					},
				}},
			},
			configs: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResourceConfiguration",
					"metadata":   map[string]interface{}{"name": "test-config", "namespace": "default"},
					"spec": map[string]interface{}{
						"authentication": map[string]interface{}{
							"bearer": map[string]interface{}{
								"tokenRef": map[string]interface{}{"name": "token-secret", "namespace": "default", "key": "token"},
							},
						},
					},
				}},
			},
			secrets: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "token-secret", "namespace": "default"},
					"data":       map[string]interface{}{"token": base64.StdEncoding.EncodeToString([]byte("test-token"))},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr: false,
			validateResult: func(t *testing.T, info *Info) {
				assert.NotNil(t, info)
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			},
		},
		{
			name: "successful retrieval with basic authentication",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": map[string]interface{}{"name": "test-config-basic"}},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			configs: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResourceConfiguration",
					"metadata":   map[string]interface{}{"name": "test-config-basic", "namespace": "default"},
					"spec": map[string]interface{}{
						"authentication": map[string]interface{}{
							"basic": map[string]interface{}{
								"username":    "test-user",
								"passwordRef": map[string]interface{}{"name": "password-secret", "namespace": "default", "key": "pwd"},
							},
						},
					},
				}},
			},
			secrets: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "password-secret", "namespace": "default"},
					"data":       map[string]interface{}{"pwd": base64.StdEncoding.EncodeToString([]byte("test-pass"))},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr: false,
			validateResult: func(t *testing.T, info *Info) {
				assert.NotNil(t, info)
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				user, pass, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "test-user", user)
				assert.Equal(t, "test-pass", pass)
			},
		},
		{
			name: "successful retrieval with cross-namespace configurationRef",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec": map[string]interface{}{
					"configurationRef": map[string]interface{}{
						"name":      "central-config",
						"namespace": "krateo-system",
					},
				},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			configs: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResourceConfiguration",
					"metadata":   map[string]interface{}{"name": "central-config", "namespace": "krateo-system"},
					"spec": map[string]interface{}{
						"authentication": map[string]interface{}{
							"bearer": map[string]interface{}{
								"tokenRef": map[string]interface{}{"name": "central-token-secret", "namespace": "krateo-system", "key": "token"},
							},
						},
					},
				}},
			},
			secrets: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "central-token-secret", "namespace": "krateo-system"},
					"data":       map[string]interface{}{"token": base64.StdEncoding.EncodeToString([]byte("central-test-token"))},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr: false,
			validateResult: func(t *testing.T, info *Info) {
				assert.NotNil(t, info)
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				assert.Equal(t, "Bearer central-test-token", req.Header.Get("Authorization"))
			},
		},
		{
			name: "error when configurationRef is malformed (not a map)",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": "not-a-map"},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
			},
			wantErr:        true,
			wantErrMessage: "getting spec.configurationRef for 'TestResource' in namespace: default",
		},
		{
			name: "error when configurationRef points to a non-existent config object",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": map[string]interface{}{"name": "non-existent-config"}},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr:        true,
			wantErrMessage: "testresourceconfigurations.test.io \"non-existent-config\" not found",
		},
		{
			name: "error when secret key is not found in secret",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": map[string]interface{}{"name": "test-config"}},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			configs: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResourceConfiguration",
					"metadata":   map[string]interface{}{"name": "test-config", "namespace": "default"},
					"spec": map[string]interface{}{
						"authentication": map[string]interface{}{
							"bearer": map[string]interface{}{
								"tokenRef": map[string]interface{}{"name": "token-secret", "namespace": "default", "key": "token"},
							},
						},
					},
				}},
			},
			secrets: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "token-secret", "namespace": "default"},
					"data":       map[string]interface{}{"wrong-key": base64.StdEncoding.EncodeToString([]byte("test-token"))},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr:        true,
			wantErrMessage: "key token not found in secret default/token-secret",
		},
		{
			name: "error when no supported auth method is found",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
				"spec":       map[string]interface{}{"configurationRef": map[string]interface{}{"name": "test-config-unsupported"}},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec":       map[string]interface{}{"resourceGroup": "test.io", "oasPath": "/oas.yaml", "resource": map[string]interface{}{"kind": "TestResource"}},
				}},
			},
			configs: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResourceConfiguration",
					"metadata":   map[string]interface{}{"name": "test-config-unsupported", "namespace": "default"},
					"spec": map[string]interface{}{
						"authentication": map[string]interface{}{
							"apiKey": map[string]interface{}{},
						},
					},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResourceConfiguration"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresourceconfigurations"}, nil)
			},
			wantErr:        true,
			wantErrMessage: "unknown auth type: apiKey",
		},
		{
			name: "error when RestDefinition resource has wrong data type",
			unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "test.io/v1",
				"kind":       "TestResource",
				"metadata":   map[string]interface{}{"name": "test-resource-instance", "namespace": "default"},
			}},
			definitions: []runtime.Object{
				&unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "ogen.krateo.io/v1alpha1",
					"kind":       "RestDefinition",
					"metadata":   map[string]interface{}{"name": "test-def"},
					"spec": map[string]interface{}{
						"resourceGroup": "test.io",
						"oasPath":       "/api/v1/oas.yaml",
						"resource": map[string]interface{}{
							"kind":        "TestResource",
							"identifiers": []interface{}{map[string]interface{}{"foo": "bar"}}, // Invalid type, should be []string
						},
					},
				}},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "TestResource"}).
					Return(schema.GroupVersionResource{Group: "test.io", Version: "v1", Resource: "testresources"}, nil)
			},
			wantErr:        true,
			wantErrMessage: "json: cannot unmarshal object into Go struct field Resource.identifiers of type string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvrToListKind := map[schema.GroupVersionResource]string{
				{
					Group:    "ogen.krateo.io",
					Version:  "v1alpha1",
					Resource: "restdefinitions",
				}: "RestDefinitionList",
			}

			// Setup initial objects
			allObjects := append(tt.definitions, tt.configs...)
			allObjects = append(allObjects, tt.secrets...)
			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind, allObjects...)

			mockPlural := &mockPluralizerInterface{}
			if tt.setupMocks != nil {
				tt.setupMocks(mockPlural)
			}

			g := &dynamicGetter{
				dynamicClient: client,
				pluralizer:    mockPlural,
			}

			result, err := g.Get(tt.unstructured)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateResult != nil {
					tt.validateResult(t, result)
				}
			}

			mockPlural.AssertExpectations(t)
		})
	}
}

func TestGetSecret(t *testing.T) {
	tests := []struct {
		name           string
		secret         *unstructured.Unstructured
		selector       SecretKeySelector
		wantValue      string
		wantErr        bool
		wantErrMessage string
	}{
		{
			name: "successful secret retrieval",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-secret",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": base64.StdEncoding.EncodeToString([]byte("secret-value")),
					},
				},
			},
			selector: SecretKeySelector{
				Name:      "test-secret",
				Namespace: "default",
				Key:       "key",
			},
			wantValue: "secret-value",
			wantErr:   false,
		},
		{
			name:   "secret not found",
			secret: nil,
			selector: SecretKeySelector{
				Name:      "nonexistent-secret",
				Namespace: "default",
				Key:       "key",
			},
			wantErr: true,
		},
		{
			name: "missing key in secret data",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "test-secret", "namespace": "default"},
					"data":       map[string]interface{}{"other-key": "dmFsdWU="},
				},
			},
			selector:       SecretKeySelector{Name: "test-secret", Namespace: "default", Key: "missing-key"},
			wantErr:        true,
			wantErrMessage: "key missing-key not found in secret default/test-secret",
		},
		{
			name: "invalid base64 encoding",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]interface{}{"name": "test-secret", "namespace": "default"},
					"data":       map[string]interface{}{"key": "invalid-base64!"},
				},
			},
			selector:       SecretKeySelector{Name: "test-secret", Namespace: "default", Key: "key"},
			wantErr:        true,
			wantErrMessage: "failed to decode secret key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *fake.FakeDynamicClient
			if tt.secret != nil {
				client = fake.NewSimpleDynamicClient(scheme.Scheme, tt.secret)
			} else {
				client = fake.NewSimpleDynamicClient(scheme.Scheme)
			}

			value, err := GetSecret(context.Background(), client, tt.selector)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantValue, value)
			}
		})
	}
}
