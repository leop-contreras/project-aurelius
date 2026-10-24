package controllers

import (
	"context"
	"errors"
	"log"
	"main/events"
	"main/kafkax"
	"main/models"
	"main/proto_gen"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var debugWorkerID = os.Getenv("DEBUG_WORKER_ID")

type ReadController struct {
	proto_gen.UnimplementedInstanceServiceServer
	InstanceModel *models.InstanceModel
}

type WriteController struct {
	proto_gen.UnimplementedInstanceServiceServer
	InstanceModel *models.InstanceModel
	Events        *kafka.Writer
}

type UpdateController struct {
	proto_gen.UnimplementedInstanceServiceServer
	InstanceModel *models.InstanceModel
	ReadClient    proto_gen.InstanceServiceClient
	Events        *kafka.Writer
}

func toProto(instance *models.Instance) *proto_gen.Instance {
	return &proto_gen.Instance{
		Id:     int32(instance.ID),
		Name:   instance.Name,
		Status: instance.Status,
	}
}

func setWorkerID(ctx context.Context) {
	grpc.SetHeader(ctx, metadata.Pairs("x-worker-id", debugWorkerID))
}

func publishEvent(w *kafka.Writer, eventType string, instance *models.Instance) {
	kafkax.Publish(w, strconv.Itoa(instance.ID), events.InstanceEvent{
		EventID:    uuid.NewString(),
		Type:       eventType,
		InstanceID: instance.ID,
		Name:       instance.Name,
		Status:     instance.Status,
		WorkerID:   debugWorkerID,
		OccurredAt: time.Now().UTC(),
	})
}

func (c *ReadController) GetInstance(ctx context.Context, req *proto_gen.GetInstanceRequest) (*proto_gen.Instance, error) {
	instance, err := c.InstanceModel.GetInstanceByID(int(req.Id))
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "Instance not found")
		}
		return nil, status.Error(codes.Internal, "Failed to get instance")
	}

	setWorkerID(ctx)
	return toProto(instance), nil
}

func (c *WriteController) CreateInstance(ctx context.Context, req *proto_gen.CreateInstanceRequest) (*proto_gen.Instance, error) {
	instance, err := c.InstanceModel.CreateInstance(req.Name)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to create instance")
	}

	publishEvent(c.Events, events.TopicInstanceCreated, instance)
	setWorkerID(ctx)
	return toProto(instance), nil
}

func (c *UpdateController) UpdateStatus(ctx context.Context, req *proto_gen.UpdateStatusRequest) (*proto_gen.Instance, error) {
	var header metadata.MD
	_, err := c.ReadClient.GetInstance(ctx, &proto_gen.GetInstanceRequest{Id: req.Id}, grpc.Header(&header))
	if err != nil {
		log.Printf("Read service check for instance %d failed: %v", req.Id, err)
		return nil, err
	}
	log.Printf("Instance %d exists, checked by read worker %v", req.Id, header.Get("x-worker-id"))

	instance, err := c.InstanceModel.UpdateStatus(int(req.Id), req.Status)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update instance status")
	}

	publishEvent(c.Events, events.TopicInstanceStatusUpdated, instance)
	setWorkerID(ctx)
	return toProto(instance), nil
}
