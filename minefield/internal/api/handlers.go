package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/squareup/proto-fleet/minefield/internal/errors"
)

// Handler handles control API requests
type Handler struct {
	errorStore *errors.Store
}

// NewHandler creates a new API handler
func NewHandler(errorStore *errors.Store) *Handler {
	return &Handler{
		errorStore: errorStore,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Error management
	router.HandleFunc("/errors/trigger", h.triggerError).Methods("POST", "OPTIONS")
	router.HandleFunc("/errors/active", h.getActiveErrors).Methods("GET")
	router.HandleFunc("/errors/all", h.getAllErrors).Methods("GET")
	router.HandleFunc("/errors/{id}", h.clearError).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/errors", h.clearAllErrors).Methods("DELETE", "OPTIONS")

	// Error definitions
	router.HandleFunc("/errors/definitions", h.getErrorDefinitions).Methods("GET")
	router.HandleFunc("/errors/categories", h.getErrorCategories).Methods("GET")

	// Status
	router.HandleFunc("/status", h.getStatus).Methods("GET")
}

// TriggerErrorRequest is the request body for triggering an error
type TriggerErrorRequest struct {
	ErrorCode      string `json:"error_code"`
	Source         string `json:"source"`           // "rig" | "fan" | "psu" | "hashboard"
	ComponentIndex *int   `json:"component_index,omitempty"`
	Message        string `json:"message,omitempty"`
	TTLSeconds     *int   `json:"ttl_seconds,omitempty"`
}

// triggerError handles POST /api/errors/trigger
func (h *Handler) triggerError(w http.ResponseWriter, r *http.Request) {
	var req TriggerErrorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate source
	validSources := map[string]bool{
		"rig":       true,
		"fan":       true,
		"psu":       true,
		"hashboard": true,
	}
	if !validSources[req.Source] {
		http.Error(w, "Invalid source. Must be one of: rig, fan, psu, hashboard", http.StatusBadRequest)
		return
	}

	// Use a default message if not provided
	if req.Message == "" {
		req.Message = "Injected error: " + req.ErrorCode
	}

	// Trigger the error
	error := h.errorStore.TriggerError(
		req.ErrorCode,
		req.Source,
		req.Message,
		req.ComponentIndex,
		req.TTLSeconds,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(error)
}

// getActiveErrors handles GET /api/errors/active
func (h *Handler) getActiveErrors(w http.ResponseWriter, r *http.Request) {
	errors := h.errorStore.GetActiveErrors()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(errors)
}

// getAllErrors handles GET /api/errors/all
func (h *Handler) getAllErrors(w http.ResponseWriter, r *http.Request) {
	errors := h.errorStore.GetAllErrors()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(errors)
}

// clearError handles DELETE /api/errors/{id}
func (h *Handler) clearError(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := h.errorStore.ClearError(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// clearAllErrors handles DELETE /api/errors
func (h *Handler) clearAllErrors(w http.ResponseWriter, r *http.Request) {
	h.errorStore.ClearAllErrors()
	w.WriteHeader(http.StatusNoContent)
}

// getErrorDefinitions handles GET /api/errors/definitions
func (h *Handler) getErrorDefinitions(w http.ResponseWriter, r *http.Request) {
	definitions := errors.GetErrorDefinitions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(definitions)
}

// getErrorCategories handles GET /api/errors/categories
func (h *Handler) getErrorCategories(w http.ResponseWriter, r *http.Request) {
	categories := errors.GetErrorCategories()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(categories)
}

// StatusResponse represents the status of the proxy
type StatusResponse struct {
	Status       string `json:"status"`
	ActiveErrors int    `json:"active_errors"`
	TotalErrors  int    `json:"total_errors"`
}

// getStatus handles GET /api/status
func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	active := h.errorStore.GetActiveErrors()
	all := h.errorStore.GetAllErrors()

	status := StatusResponse{
		Status:       "running",
		ActiveErrors: len(active),
		TotalErrors:  len(all),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}