package getter

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	restclient "github.com/krateoplatformops/rest-dynamic-controller/internal/tools/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	tests := []struct {
		name    string
		cfg     interface{}
		wantErr bool
	}{
		{
			name:    "nil config should return error",
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPlural := &mockPluralizerInterface{}
			_, err := Dynamic(nil, mockPlural)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDynamicGetter_Get(t *testing.T) {
	tests := []struct {
		name           string
		unstructured   *unstructured.Unstructured
		definitions    []unstructured.Unstructured
		setupMocks     func(*mockPluralizerInterface)
		wantErr        bool
		wantErrMessage string
	}{
		{
			name: "no definitions found",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "no definitions found",
		},
		{
			name: "missing spec.resource in definition",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "test.io",
							"oasPath":       "/api/v1/swagger.json",
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "missing spec.resources in definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create custom list kinds to register the RestDefinition resource
			gvrToListKind := map[schema.GroupVersionResource]string{
				{
					Group:    "swaggergen.krateo.io",
					Version:  "v1alpha1",
					Resource: "restdefinitions",
				}: "RestDefinitionList",
			}

			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind)

			// Add definitions to fake client
			gvrForDefinitions := schema.GroupVersionResource{
				Group:    "swaggergen.krateo.io",
				Version:  "v1alpha1",
				Resource: "restdefinitions",
			}

			for _, def := range tt.definitions {
				_, err := client.Resource(gvrForDefinitions).Create(context.Background(), &def, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			mockPlural := &mockPluralizerInterface{}
			if tt.setupMocks != nil {
				tt.setupMocks(mockPlural)
			}

			g := &dynamicGetter{
				dynamicClient: client,
				pluralizer:    mockPlural,
			}

			_, err := g.Get(tt.unstructured)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
			}

			mockPlural.AssertExpectations(t)
		})
	}
}

func TestParseAuthentication(t *testing.T) {
	tests := []struct {
		name           string
		authType       restclient.AuthType
		authObject     *unstructured.Unstructured
		secrets        []unstructured.Unstructured
		wantErr        bool
		wantErrMessage string
		validateAuth   func(*testing.T, *Info)
	}{
		{
			name:     "basic auth success",
			authType: restclient.AuthTypeBasic,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BasicAuth",
					"metadata": map[string]interface{}{
						"name":      "basic-auth",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"username": "testuser",
						"passwordRef": map[string]interface{}{
							"name":      "password-secret",
							"namespace": "default",
							"key":       "password",
						},
					},
				},
			},
			secrets: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "password-secret",
							"namespace": "default",
						},
						"data": map[string]interface{}{
							"password": base64.StdEncoding.EncodeToString([]byte("testpass")),
						},
					},
				},
			},
			wantErr: false,
			validateAuth: func(t *testing.T, info *Info) {
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				username, password, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "testuser", username)
				assert.Equal(t, "testpass", password)
			},
		},
		{
			name:     "bearer auth success",
			authType: restclient.AuthTypeBearer,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BearerAuth",
					"metadata": map[string]interface{}{
						"name":      "bearer-auth",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"tokenRef": map[string]interface{}{
							"name":      "token-secret",
							"namespace": "default",
							"key":       "token",
						},
					},
				},
			},
			secrets: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "token-secret",
							"namespace": "default",
						},
						"data": map[string]interface{}{
							"token": base64.StdEncoding.EncodeToString([]byte("test-token")),
						},
					},
				},
			},
			wantErr: false,
			validateAuth: func(t *testing.T, info *Info) {
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			},
		},
		{
			name:     "missing username in basic auth",
			authType: restclient.AuthTypeBasic,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BasicAuth",
					"spec": map[string]interface{}{
						"passwordRef": map[string]interface{}{
							"name": "password-secret",
						},
					},
				},
			},
			wantErr:        true,
			wantErrMessage: "missing spec.username",
		},
		{
			name:     "unknown auth type",
			authType: restclient.AuthType("unknown"),
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "UnknownAuth",
				},
			},
			wantErr:        true,
			wantErrMessage: "unknown auth type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(scheme.Scheme)

			// Add secrets to fake client
			secretsGVR := schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			}

			for _, secret := range tt.secrets {
				_, err := client.Resource(secretsGVR).Namespace("default").Create(context.Background(), &secret, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			info := &Info{}
			err := parseAuthentication(tt.authObject, tt.authType, client, info)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateAuth != nil {
					tt.validateAuth(t, info)
				}
			}
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
			name: "invalid base64 encoding",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-secret",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": "invalid-base64!",
					},
				},
			},
			selector: SecretKeySelector{
				Name:      "test-secret",
				Namespace: "default",
				Key:       "key",
			},
			wantErr:        true,
			wantErrMessage: "failed to decode secret key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(scheme.Scheme)

			if tt.secret != nil {
				gvr := schema.GroupVersionResource{
					Group:    "",
					Version:  "v1",
					Resource: "secrets",
				}
				_, err := client.Resource(gvr).Namespace(tt.selector.Namespace).Create(context.Background(), tt.secret, metav1.CreateOptions{})
				assert.NoError(t, err)
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

func TestDynamicGetter_Get_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		name           string
		unstructured   *unstructured.Unstructured
		definitions    []unstructured.Unstructured
		setupMocks     func(*mockPluralizerInterface)
		wantErr        bool
		wantErrMessage string
		validateResult func(*testing.T, *Info)
	}{
		{
			name: "successful definition retrieval with matching kind",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestResource",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "test.io",
							"oasPath":       "/api/v1/swagger.json",
							"resource": map[string]interface{}{
								"kind":        "TestResource",
								"identifiers": []interface{}{"id"}, // Changed from []string to []interface{}
								"verbsDescription": []interface{}{
									map[string]interface{}{
										"action": "get",
										"method": "GET",
										"path":   "/api/v1/resources/{id}",
									},
								},
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestResource",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testresources",
				}, nil)
			},
			wantErr: false,
			validateResult: func(t *testing.T, info *Info) {
				assert.Equal(t, "/api/v1/swagger.json", info.URL)
				assert.Equal(t, "TestResource", info.Resource.Kind)
				assert.Equal(t, []string{"id"}, info.Resource.Identifiers)
				assert.Len(t, info.Resource.VerbsDescription, 1)
				assert.Equal(t, "get", info.Resource.VerbsDescription[0].Action)
			},
		},
		{
			name: "missing spec.resourceGroup",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"oasPath": "/api/v1/swagger.json",
							"resource": map[string]interface{}{
								"kind": "TestKind",
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "missing spec.resourceGroup",
		},
		{
			name: "missing kind in resource definition",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "test.io",
							"oasPath":       "/api/v1/swagger.json",
							"resource": map[string]interface{}{
								"identifiers": []interface{}{"id"}, // Changed from []string to []interface{}
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "missing kind in definition",
		},
		{
			name: "missing spec.oasPath",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "test.io",
							"resource": map[string]interface{}{
								"kind": "TestKind",
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "missing spec.oasPath",
		},
		{
			name: "kind mismatch - should continue searching",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "test.io",
							"oasPath":       "/api/v1/swagger.json",
							"resource": map[string]interface{}{
								"kind": "DifferentKind",
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "no definitions found",
		},
		{
			name: "group mismatch - should continue searching",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "swaggergen.krateo.io/v1alpha1",
						"kind":       "RestDefinition",
						"spec": map[string]interface{}{
							"resourceGroup": "different.io",
							"oasPath":       "/api/v1/swagger.json",
							"resource": map[string]interface{}{
								"kind": "TestKind",
							},
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "no definitions found",
		},
		{
			name: "pluralizer error",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			definitions: []unstructured.Unstructured{},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{}, fmt.Errorf("pluralizer error"))
			},
			wantErr:        true,
			wantErrMessage: "getting GVR for 'TestKind' in namespace:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvrToListKind := map[schema.GroupVersionResource]string{
				{
					Group:    "swaggergen.krateo.io",
					Version:  "v1alpha1",
					Resource: "restdefinitions",
				}: "RestDefinitionList",
			}

			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind)

			gvrForDefinitions := schema.GroupVersionResource{
				Group:    "swaggergen.krateo.io",
				Version:  "v1alpha1",
				Resource: "restdefinitions",
			}

			for _, def := range tt.definitions {
				_, err := client.Resource(gvrForDefinitions).Create(context.Background(), &def, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

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

func TestDynamicGetter_SetAuth(t *testing.T) {
	tests := []struct {
		name           string
		unstructured   *unstructured.Unstructured
		authObjects    []unstructured.Unstructured
		secrets        []unstructured.Unstructured
		setupMocks     func(*mockPluralizerInterface)
		wantErr        bool
		wantErrMessage string
		validateAuth   func(*testing.T, *Info)
	}{
		{
			name: "no authentication refs - should not error",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
					"spec": map[string]interface{}{},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr: false,
		},
		{
			name: "bearer auth with valid token",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"authenticationRefs": map[string]interface{}{
							"bearerAuthRef": "bearer-auth",
						},
					},
				},
			},
			authObjects: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "test.io/v1alpha1",
						"kind":       "BearerAuth",
						"metadata": map[string]interface{}{
							"name":      "bearer-auth",
							"namespace": "default",
						},
						"spec": map[string]interface{}{
							"tokenRef": map[string]interface{}{
								"name":      "token-secret",
								"namespace": "default",
								"key":       "token",
							},
						},
					},
				},
			},
			secrets: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "token-secret",
							"namespace": "default",
						},
						"data": map[string]interface{}{
							"token": base64.StdEncoding.EncodeToString([]byte("test-token")),
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1alpha1",
					Kind:    "BearerAuth",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1alpha1",
					Resource: "bearerauths",
				}, nil)
			},
			wantErr: false,
			validateAuth: func(t *testing.T, info *Info) {
				assert.NotNil(t, info.SetAuth)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				info.SetAuth(req)
				assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
			},
		},
		{
			name: "authentication ref error - nested field error",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"authenticationRefs": "invalid-type", // Should be map, not string
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "getting spec.authenticationRefs",
		},
		{
			name: "auth object not found",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"authenticationRefs": map[string]interface{}{
							"basicAuthRef": "nonexistent-auth",
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1alpha1",
					Kind:    "BasicAuth",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1alpha1",
					Resource: "basicauths",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "getting authentication",
		},
		{
			name: "invalid auth type",
			unstructured: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"authenticationRefs": map[string]interface{}{
							"invalidAuthRef": "invalid-auth",
						},
					},
				},
			},
			setupMocks: func(m *mockPluralizerInterface) {
				m.On("GVKtoGVR", schema.GroupVersionKind{
					Group:   "test.io",
					Version: "v1",
					Kind:    "TestKind",
				}).Return(schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1",
					Resource: "testkinds",
				}, nil)
			},
			wantErr:        true,
			wantErrMessage: "unknown auth type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvrToListKind := map[schema.GroupVersionResource]string{
				{
					Group:    "test.io",
					Version:  "v1alpha1",
					Resource: "basicauths",
				}: "BasicAuthList",
				{
					Group:    "test.io",
					Version:  "v1alpha1",
					Resource: "bearerauths",
				}: "BearerAuthList",
			}

			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind)

			// Add auth objects
			for _, auth := range tt.authObjects {
				gvr := schema.GroupVersionResource{
					Group:    "test.io",
					Version:  "v1alpha1",
					Resource: strings.ToLower(auth.GetKind()) + "s",
				}
				_, err := client.Resource(gvr).Namespace("default").Create(context.Background(), &auth, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			// Add secrets
			secretsGVR := schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			}
			for _, secret := range tt.secrets {
				_, err := client.Resource(secretsGVR).Namespace("default").Create(context.Background(), &secret, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			mockPlural := &mockPluralizerInterface{}
			if tt.setupMocks != nil {
				tt.setupMocks(mockPlural)
			}

			g := &dynamicGetter{
				dynamicClient: client,
				pluralizer:    mockPlural,
			}

			info := &Info{}
			err := g.setAuth(tt.unstructured, info)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateAuth != nil {
					tt.validateAuth(t, info)
				}
			}

			mockPlural.AssertExpectations(t)
		})
	}
}

func TestGetSecret_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		name           string
		secret         *unstructured.Unstructured
		selector       SecretKeySelector
		wantValue      string
		wantErr        bool
		wantErrMessage string
	}{
		{
			name: "missing key in secret data",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-secret",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"other-key": base64.StdEncoding.EncodeToString([]byte("other-value")),
					},
				},
			},
			selector: SecretKeySelector{
				Name:      "test-secret",
				Namespace: "default",
				Key:       "missing-key",
			},
			wantErr: true,
		},
		{
			name: "empty data field",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-secret",
						"namespace": "default",
					},
					"data": map[string]interface{}{},
				},
			},
			selector: SecretKeySelector{
				Name:      "test-secret",
				Namespace: "default",
				Key:       "key",
			},
			wantErr: true,
		},
		{
			name: "missing data field entirely",
			secret: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-secret",
						"namespace": "default",
					},
				},
			},
			selector: SecretKeySelector{
				Name:      "test-secret",
				Namespace: "default",
				Key:       "key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(scheme.Scheme)

			if tt.secret != nil {
				gvr := schema.GroupVersionResource{
					Group:    "",
					Version:  "v1",
					Resource: "secrets",
				}
				_, err := client.Resource(gvr).Namespace(tt.selector.Namespace).Create(context.Background(), tt.secret, metav1.CreateOptions{})
				assert.NoError(t, err)
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

func TestParseAuthentication_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		name           string
		authType       restclient.AuthType
		authObject     *unstructured.Unstructured
		secrets        []unstructured.Unstructured
		wantErr        bool
		wantErrMessage string
		validateAuth   func(*testing.T, *Info)
	}{
		{
			name:     "missing spec.passwordRef in basic auth",
			authType: restclient.AuthTypeBasic,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BasicAuth",
					"spec": map[string]interface{}{
						"username": "testuser",
					},
				},
			},
			wantErr:        true,
			wantErrMessage: "missing spec.passwordRef",
		},
		{
			name:     "secret not found for basic auth",
			authType: restclient.AuthTypeBasic,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BasicAuth",
					"spec": map[string]interface{}{
						"username": "testuser",
						"passwordRef": map[string]interface{}{
							"name":      "nonexistent-secret",
							"namespace": "default",
							"key":       "password",
						},
					},
				},
			},
			wantErr:        true,
			wantErrMessage: "getting password",
		},
		{
			name:     "secret not found for bearer auth",
			authType: restclient.AuthTypeBearer,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BearerAuth",
					"spec": map[string]interface{}{
						"tokenRef": map[string]interface{}{
							"name":      "nonexistent-secret",
							"namespace": "default",
							"key":       "token",
						},
					},
				},
			},
			wantErr:        true,
			wantErrMessage: "getting token",
		},
		{
			name:     "missing spec.tokenRef in bearer auth",
			authType: restclient.AuthTypeBearer,
			authObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "test.io/v1alpha1",
					"kind":       "BearerAuth",
					"spec":       map[string]interface{}{},
				},
			},
			wantErr:        true,
			wantErrMessage: "missing spec.tokenRef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(scheme.Scheme)

			// Add secrets to fake client
			secretsGVR := schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			}

			for _, secret := range tt.secrets {
				_, err := client.Resource(secretsGVR).Namespace("default").Create(context.Background(), &secret, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			info := &Info{}
			err := parseAuthentication(tt.authObject, tt.authType, client, info)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMessage != "" {
					assert.Contains(t, err.Error(), tt.wantErrMessage)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateAuth != nil {
					tt.validateAuth(t, info)
				}
			}
		})
	}
}
