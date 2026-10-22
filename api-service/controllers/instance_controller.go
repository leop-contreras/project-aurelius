package controllers

import (
	"encoding/json"
	"main/proto_gen"
	"net/http"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type InstanceController struct {
	Client proto_gen.InstanceServiceClient
}

type instance struct {
	ID     int32  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func writeInstance(w http.ResponseWriter, header metadata.MD, in *proto_gen.Instance) {
	if workerID := header.Get("x-worker-id"); len(workerID) > 0 {
		w.Header().Set("X-Worker-ID", workerID[0])
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instance{ID: in.Id, Name: in.Name, Status: in.Status})
}

func writeError(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	code := http.StatusInternalServerError
	switch st.Code() {
	case codes.InvalidArgument:
		code = http.StatusBadRequest
	case codes.NotFound:
		code = http.StatusNotFound
	case codes.Unavailable:
		code = http.StatusBadGateway
	}
	http.Error(w, st.Message(), code)
}

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

	var header metadata.MD
	instance, err := c.Client.GetInstance(r.Context(), &proto_gen.GetInstanceRequest{Id: int32(id)}, grpc.Header(&header))
	if err != nil {
		writeError(w, err)
		return
	}

	writeInstance(w, header, instance)
}

func (c *InstanceController) CreateInstanceHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var header metadata.MD
	instance, err := c.Client.CreateInstance(r.Context(), &proto_gen.CreateInstanceRequest{Name: input.Name}, grpc.Header(&header))
	if err != nil {
		writeError(w, err)
		return
	}

	writeInstance(w, header, instance)
}

func (c *InstanceController) UpdateStatusHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     int32  `json:"id"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var header metadata.MD
	instance, err := c.Client.UpdateStatus(r.Context(), &proto_gen.UpdateStatusRequest{Id: input.ID, Status: input.Status}, grpc.Header(&header))
	if err != nil {
		writeError(w, err)
		return
	}

	writeInstance(w, header, instance)
}
