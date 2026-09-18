package controllers

import (
	"encoding/json"
	"errors"
	"main/models"
	"net/http"
	"os"
	"strconv"
)

type InstanceController struct {
	InstanceModel *models.InstanceModel
}

var ErrNotFound = errors.New("instance not found")
var debugWorkerID = os.Getenv("DEBUG_WORKER_ID")

func (c *InstanceController) GetInstanceHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")

	if idStr == "" {
		http.Error(w, "Missing instance ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	instance, err := c.InstanceModel.GetInstanceByID(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Instance not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to get instance", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("X-Worker-ID", debugWorkerID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instance)
}

func (c *InstanceController) CreateInstanceHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	instance, err := c.InstanceModel.CreateInstance(input.Name)
	if err != nil {
		http.Error(w, "Failed to create instance", http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-Worker-ID", debugWorkerID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instance)
}

func (c *InstanceController) UpdateStatusHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	instance, err := c.InstanceModel.UpdateStatus(input.ID, input.Status)
	if err != nil {
		http.Error(w, "Failed to update instance status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-Worker-ID", debugWorkerID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instance)
}
