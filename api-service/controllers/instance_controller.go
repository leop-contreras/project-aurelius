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
	ID          int32  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Port        int32  `json:"port"`
	ExpiresAt   string `json:"expires_at"`
	RenewCount  int32  `json:"renew_count"`
	ChallengeID int32  `json:"challenge_id"`
}

func toJSON(in *proto_gen.Instance) instance {
	return instance{ID: in.Id, Name: in.Name, Status: in.Status, Port: in.Port, ExpiresAt: in.ExpiresAt, RenewCount: in.RenewCount, ChallengeID: in.ChallengeId}
}

func writeJSON(w http.ResponseWriter, header metadata.MD, body any) {
	if workerID := header.Get("x-worker-id"); len(workerID) > 0 {
		w.Header().Set("X-Worker-ID", workerID[0])
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func writeInstance(w http.ResponseWriter, header metadata.MD, in *proto_gen.Instance) {
	writeJSON(w, header, toJSON(in))
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
		ChallengeID int32 `json:"challenge_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var header metadata.MD
	instance, err := c.Client.CreateInstance(r.Context(), &proto_gen.CreateInstanceRequest{ChallengeId: input.ChallengeID}, grpc.Header(&header))
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

func (c *InstanceController) ListInstancesHandler(w http.ResponseWriter, r *http.Request) {
	var header metadata.MD
	list, err := c.Client.ListInstances(r.Context(), &proto_gen.ListInstancesRequest{}, grpc.Header(&header))
	if err != nil {
		writeError(w, err)
		return
	}

	instances := []instance{}
	for _, in := range list.Instances {
		instances = append(instances, toJSON(in))
	}

	writeJSON(w, header, instances)
}

func (c *InstanceController) RenewInstanceHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID int32 `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var header metadata.MD
	instance, err := c.Client.RenewInstance(r.Context(), &proto_gen.RenewInstanceRequest{Id: input.ID}, grpc.Header(&header))
	if err != nil {
		writeError(w, err)
		return
	}

	writeInstance(w, header, instance)
}
