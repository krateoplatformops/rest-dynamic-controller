package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Resource struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
}

type MockServer struct {
	resources map[string]*Resource
	pending   map[string]*Resource // Resources in pending state for async operations
	mutex     sync.RWMutex
	port      int

	// Test configuration flags
	simulateErrors  bool
	responseDelay   time.Duration
	authFailures    bool
	asyncOperations bool
}

func NewMockServer(port int) *MockServer {
	return &MockServer{
		resources: make(map[string]*Resource),
		pending:   make(map[string]*Resource),
		port:      port,
	}
}

func (ms *MockServer) authenticate(r *http.Request) bool {
	if ms.authFailures {
		return false
	}

	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, "Bearer test") || auth == "Bearer test"
}

func (ms *MockServer) delay() {
	if ms.responseDelay > 0 {
		time.Sleep(ms.responseDelay)
	}
}

func (ms *MockServer) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (ms *MockServer) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// GET /resource - Lista risorse con parametri di query opzionali
func (ms *MockServer) listResources(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	// Supporta filtri per nome (come usato nei test)
	nameFilter := r.URL.Query().Get("name")

	var result []*Resource
	for _, resource := range ms.resources {
		if nameFilter == "" || resource.Name == nameFilter {
			result = append(result, resource)
		}
	}

	// Se non trova nessuna risorsa con quel nome, restituisce 404
	if nameFilter != "" && len(result) == 0 {
		ms.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	// Formato lista
	ms.writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": result,
		"count": len(result),
	})
}

// GET /resource/{id} - Ottiene risorsa specifica per ID
func (ms *MockServer) getResource(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	if id == "" {
		ms.writeError(w, http.StatusBadRequest, "missing resource id")
		return
	}

	ms.mutex.RLock()
	fmt.Println("Looking for resource with ID:", id)
	resource, exists := ms.resources[id]
	if !exists {
		fmt.Println("Resource not found in resources map, checking pending", id)
		resource, exists = ms.pending[id]
		fmt.Println("Pending resource found:", exists, "for ID:", id)
	}
	ms.mutex.RUnlock()

	if !exists {
		ms.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	ms.writeJSON(w, http.StatusOK, resource)
}

// POST /resource - Crea nuova risorsa
func (ms *MockServer) createResource(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	var resource Resource
	if err := json.NewDecoder(r.Body).Decode(&resource); err != nil {
		ms.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if resource.Name == "" {
		ms.writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	// Genera ID se non fornito
	if resource.ID == "" {
		resource.ID = resource.Name // Usa il nome come ID per i test
	}

	// Controlla se la risorsa esiste già
	if _, exists := ms.resources[resource.ID]; exists {
		ms.writeError(w, http.StatusConflict, "resource already exists")
		return
	}

	// Imposta timestamps e stato di default
	now := time.Now().UTC().Format(time.RFC3339)
	resource.CreatedAt = now
	resource.UpdatedAt = now
	if resource.Status == "" {
		resource.Status = "created"
	}

	// Supporta operazioni asincrone (202 Accepted)
	if ms.asyncOperations {
		ms.pending[resource.ID] = &resource // <-- store in pending
		ms.writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"operationId": fmt.Sprintf("op-%d", time.Now().Unix()),
			"status":      "pending",
			"resource":    resource,
		})
		return
	}

	ms.resources[resource.ID] = &resource
	ms.writeJSON(w, http.StatusCreated, resource)
}

// PUT /resource/{id} - Aggiorna risorsa esistente
func (ms *MockServer) updateResource(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	if id == "" {
		ms.writeError(w, http.StatusBadRequest, "missing resource id")
		return
	}

	var updateData Resource
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		ms.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	resource, exists := ms.resources[id]
	if !exists {
		ms.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	// Aggiorna campi
	if updateData.Name != "" {
		resource.Name = updateData.Name
	}
	if updateData.Description != "" {
		resource.Description = updateData.Description
	}
	if updateData.Status != "" {
		resource.Status = updateData.Status
	}
	if updateData.Metadata != nil {
		if resource.Metadata == nil {
			resource.Metadata = make(map[string]string)
		}
		for k, v := range updateData.Metadata {
			resource.Metadata[k] = v
		}
	}

	resource.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Supporta operazioni asincrone
	if ms.asyncOperations {
		ms.writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"operationId": fmt.Sprintf("op-update-%d", time.Now().Unix()),
			"status":      "pending",
			"resource":    resource,
		})
		return
	}

	ms.writeJSON(w, http.StatusOK, resource)
}

// PATCH /resource/{id} - Aggiorna parzialmente risorsa
func (ms *MockServer) patchResource(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	if id == "" {
		ms.writeError(w, http.StatusBadRequest, "missing resource id")
		return
	}

	var patchData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patchData); err != nil {
		ms.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	resource, exists := ms.resources[id]
	if !exists {
		ms.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	// Applica patch
	if name, ok := patchData["name"].(string); ok {
		resource.Name = name
	}
	if description, ok := patchData["description"].(string); ok {
		resource.Description = description
	}
	if status, ok := patchData["status"].(string); ok {
		resource.Status = status
	}

	resource.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	ms.writeJSON(w, http.StatusOK, resource)
}

// DELETE /resource/{id} - Elimina risorsa
func (ms *MockServer) deleteResource(w http.ResponseWriter, r *http.Request) {
	ms.delay()

	if !ms.authenticate(r) {
		ms.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if ms.simulateErrors {
		ms.writeError(w, http.StatusInternalServerError, "simulated server error")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	if id == "" {
		ms.writeError(w, http.StatusBadRequest, "missing resource id")
		return
	}

	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if _, exists := ms.resources[id]; !exists {
		ms.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	delete(ms.resources, id)

	// Supporta operazioni asincrone
	if ms.asyncOperations {
		ms.writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"operationId": fmt.Sprintf("op-delete-%d", time.Now().Unix()),
			"status":      "pending",
		})
		return
	}

	// Restituisce 204 No Content per eliminazione riuscita
	w.WriteHeader(http.StatusNoContent)
}

// GET /health - Health check endpoint
func (ms *MockServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	ms.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "healthy",
		"time":             time.Now().UTC().Format(time.RFC3339),
		"resources_count":  len(ms.resources),
		"simulate_errors":  ms.simulateErrors,
		"auth_failures":    ms.authFailures,
		"async_operations": ms.asyncOperations,
		"response_delay":   ms.responseDelay.String(),
	})
}

// POST /admin/config - Configura comportamento del server per i test
func (ms *MockServer) configureServer(w http.ResponseWriter, r *http.Request) {
	var config struct {
		SimulateErrors       *bool `json:"simulateErrors,omitempty"`
		ResponseDelay        *int  `json:"responseDelayMs,omitempty"`
		AuthFailures         *bool `json:"authFailures,omitempty"`
		AsyncOperations      *bool `json:"asyncOperations,omitempty"`
		CompletePendingAsync *bool `json:"completePendingAsync,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		ms.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	b, _ := json.MarshalIndent(config, "", "  ")
	fmt.Println("Received configuration:", string(b))

	if config.CompletePendingAsync != nil && *config.CompletePendingAsync {
		ms.mutex.Lock()
		for id, res := range ms.pending {
			fmt.Println("Completing pending async operation for resource:", id)
			ms.resources[id] = res

			b, _ := json.MarshalIndent(ms.resources, "", "  ")
			fmt.Println("Current resources after completing pending:", string(b))

		}
		ms.pending = make(map[string]*Resource)
		ms.mutex.Unlock()
	}
	if config.SimulateErrors != nil {
		ms.simulateErrors = *config.SimulateErrors
	}
	if config.ResponseDelay != nil {
		ms.responseDelay = time.Duration(*config.ResponseDelay) * time.Millisecond
	}
	if config.AuthFailures != nil {
		ms.authFailures = *config.AuthFailures
	}
	if config.AsyncOperations != nil {
		ms.asyncOperations = *config.AsyncOperations
	}

	ms.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "configuration updated",
		"config": map[string]interface{}{
			"simulateErrors":  ms.simulateErrors,
			"responseDelay":   ms.responseDelay.String(),
			"authFailures":    ms.authFailures,
			"asyncOperations": ms.asyncOperations,
		},
	})
}

// GET /status/{code} - Restituisce codice di stato specifico per test
func (ms *MockServer) statusCode(w http.ResponseWriter, r *http.Request) {
	codeStr := strings.TrimPrefix(r.URL.Path, "/status/")
	code, err := strconv.Atoi(codeStr)
	if err != nil {
		ms.writeError(w, http.StatusBadRequest, "invalid status code")
		return
	}

	switch code {
	case http.StatusNoContent, http.StatusNotModified:
		w.WriteHeader(code)
		return
	case http.StatusAccepted:
		ms.writeJSON(w, code, map[string]interface{}{
			"status":  "pending",
			"message": "request accepted for processing",
		})
		return
	default:
		w.WriteHeader(code)
		if code >= 400 {
			ms.writeJSON(w, code, map[string]interface{}{
				"error": fmt.Sprintf("simulated error with status %d", code),
			})
		} else {
			ms.writeJSON(w, code, map[string]interface{}{
				"status": fmt.Sprintf("simulated response with status %d", code),
			})
		}
	}
}

func (ms *MockServer) router(w http.ResponseWriter, r *http.Request) {
	// Abilita CORS per i test
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch {
	case r.URL.Path == "/health":
		ms.healthCheck(w, r)
	case r.URL.Path == "/admin/config" && r.Method == http.MethodPost:
		ms.configureServer(w, r)
	case strings.HasPrefix(r.URL.Path, "/status/"):
		ms.statusCode(w, r)
	case r.URL.Path == "/resource":
		switch r.Method {
		case http.MethodGet:
			ms.listResources(w, r)
		case http.MethodPost:
			ms.createResource(w, r)
		default:
			ms.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case strings.HasPrefix(r.URL.Path, "/resource/"):
		switch r.Method {
		case http.MethodGet:
			ms.getResource(w, r)
		case http.MethodPut:
			ms.updateResource(w, r)
		case http.MethodPatch:
			ms.patchResource(w, r)
		case http.MethodDelete:
			ms.deleteResource(w, r)
		default:
			ms.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		ms.writeError(w, http.StatusNotFound, "endpoint not found")
	}
}

func (ms *MockServer) Start() error {
	// Pre-popola con dati di test che corrispondono a quelli attesi nei test
	ms.mutex.Lock()
	ms.mutex.Unlock()

	http.HandleFunc("/", ms.router)

	addr := fmt.Sprintf(":%d", ms.port)
	log.Printf("Mock server starting on %s", addr)
	log.Printf("Endpoints available:")
	log.Printf("  GET    /health")
	log.Printf("  POST   /admin/config (configure server behavior)")
	log.Printf("  GET    /resource (list/search resources)")
	log.Printf("  POST   /resource (create resource)")
	log.Printf("  GET    /resource/{id}")
	log.Printf("  PUT    /resource/{id}")
	log.Printf("  PATCH  /resource/{id}")
	log.Printf("  DELETE /resource/{id}")
	log.Printf("  GET    /status/{code} (return specific status code)")

	return http.ListenAndServe(addr, nil)
}

func main() {
	port := 30007
	server := NewMockServer(port)

	log.Fatal(server.Start())
}
