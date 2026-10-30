package controllers

import (
	"context"
	"errors"
	"log"
	"main/dockerx"
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
	InstanceModel  *models.InstanceModel
	ChallengeModel *models.ChallengeModel
}

type WriteController struct {
	proto_gen.UnimplementedInstanceServiceServer
	InstanceModel  *models.InstanceModel
	ChallengeModel *models.ChallengeModel
	Docker         *dockerx.Client
	MaxContainers  int
	Events         *kafka.Writer
}

type UpdateController struct {
	proto_gen.UnimplementedInstanceServiceServer
	InstanceModel *models.InstanceModel
	Docker        *dockerx.Client
	ReadClient    proto_gen.InstanceServiceClient
	Events        *kafka.Writer
}

func toProto(instance *models.Instance) *proto_gen.Instance {
	return &proto_gen.Instance{
		Id:          int32(instance.ID),
		Name:        instance.Name,
		Status:      instance.Status,
		Port:        int32(instance.Port),
		ExpiresAt:   instance.ExpiresAt.UTC().Format(time.RFC3339),
		RenewCount:  int32(instance.RenewCount),
		ChallengeId: int32(instance.ChallengeID),
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

func (c *ReadController) ListInstances(ctx context.Context, req *proto_gen.ListInstancesRequest) (*proto_gen.InstanceList, error) {
	instances, err := c.InstanceModel.ListInstances()
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to list instances")
	}

	list := &proto_gen.InstanceList{}
	for _, instance := range instances {
		list.Instances = append(list.Instances, toProto(instance))
	}

	setWorkerID(ctx)
	return list, nil
}

func (c *WriteController) CreateInstance(ctx context.Context, req *proto_gen.CreateInstanceRequest) (*proto_gen.Instance, error) {
	challenge, err := c.ChallengeModel.GetChallengeByID(int(req.ChallengeId))
	if err != nil {
		if errors.Is(err, models.ErrChallengeNotFound) {
			return nil, status.Error(codes.NotFound, "Challenge not found")
		}
		return nil, status.Error(codes.Internal, "Failed to get challenge")
	}

	active, err := c.InstanceModel.CountActive()
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to create instance")
	}
	if active >= c.MaxContainers {
		return nil, status.Error(codes.ResourceExhausted, "Max container count exceeded")
	}

	instance, err := c.InstanceModel.CreateInstance(challenge.Name, challenge.ID)
	if err != nil {
		if errors.Is(err, models.ErrNoPorts) {
			return nil, status.Error(codes.ResourceExhausted, "No free ports")
		}
		return nil, status.Error(codes.Internal, "Failed to create instance")
	}

	if err := c.Docker.PullImage(); err != nil {
		log.Printf("Failed to pull image: %v", err)
	}

	if err := c.Docker.RunContainer(instance.ID, instance.Name, instance.Port); err != nil {
		log.Printf("Failed to start container for instance %d: %v", instance.ID, err)
		if err := c.Docker.RemoveContainer(instance.ID); err != nil {
			log.Printf("Failed to remove container for instance %d: %v", instance.ID, err)
		}
		if err := c.InstanceModel.DeleteInstance(instance.ID); err != nil {
			log.Printf("Failed to delete instance %d: %v", instance.ID, err)
		}
		return nil, status.Error(codes.Internal, "Failed to start container")
	}
	log.Printf("Instance %d running as %s on port %d", instance.ID, dockerx.ContainerName(instance.ID), instance.Port)

	publishEvent(c.Events, events.TopicInstanceCreated, instance)
	setWorkerID(ctx)
	return toProto(instance), nil
}

func (c *UpdateController) checkInstance(ctx context.Context, id int32) (*proto_gen.Instance, error) {
	var header metadata.MD
	current, err := c.ReadClient.GetInstance(ctx, &proto_gen.GetInstanceRequest{Id: id}, grpc.Header(&header))
	if err != nil {
		log.Printf("Read service check for instance %d failed: %v", id, err)
		return nil, err
	}
	log.Printf("Instance %d exists, checked by read worker %v", id, header.Get("x-worker-id"))
	return current, nil
}

func (c *UpdateController) UpdateStatus(ctx context.Context, req *proto_gen.UpdateStatusRequest) (*proto_gen.Instance, error) {
	if req.Status != "active" && req.Status != "inactive" {
		return nil, status.Error(codes.InvalidArgument, "Invalid status")
	}

	current, err := c.checkInstance(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	if current.Status == req.Status {
		setWorkerID(ctx)
		return current, nil
	}
	if req.Status == "active" {
		return nil, status.Error(codes.FailedPrecondition, "Inactive instances cannot be reactivated")
	}

	if err := c.Docker.RemoveContainer(int(req.Id)); err != nil {
		log.Printf("Failed to remove container for instance %d: %v", req.Id, err)
		return nil, status.Error(codes.Internal, "Failed to remove container")
	}

	instance, err := c.InstanceModel.UpdateStatus(int(req.Id), req.Status)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update instance status")
	}
	log.Printf("Instance %d inactive, container removed", instance.ID)

	publishEvent(c.Events, events.TopicInstanceStatusUpdated, instance)
	setWorkerID(ctx)
	return toProto(instance), nil
}

func (c *UpdateController) RenewInstance(ctx context.Context, req *proto_gen.RenewInstanceRequest) (*proto_gen.Instance, error) {
	current, err := c.checkInstance(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	if current.Status != "active" {
		return nil, status.Error(codes.FailedPrecondition, "Inactive instances cannot be renewed")
	}
	if current.RenewCount >= models.MaxRenews {
		return nil, status.Error(codes.FailedPrecondition, "Max renewal count exceeded")
	}

	instance, err := c.InstanceModel.RenewInstance(int(req.Id))
	if err != nil {
		if errors.Is(err, models.ErrCannotRenew) {
			return nil, status.Error(codes.FailedPrecondition, "Instance cannot be renewed")
		}
		return nil, status.Error(codes.Internal, "Failed to renew instance")
	}

	setWorkerID(ctx)
	return toProto(instance), nil
}

func (c *UpdateController) AutoClean() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		expired, err := c.InstanceModel.GetExpired()
		if err != nil {
			log.Printf("Failed to get expired instances: %v", err)
			continue
		}

		for _, instance := range expired {
			if err := c.Docker.RemoveContainer(instance.ID); err != nil {
				log.Printf("Failed to remove container for instance %d: %v", instance.ID, err)
				continue
			}
			if _, err := c.InstanceModel.UpdateStatus(instance.ID, "inactive"); err != nil {
				log.Printf("Failed to update instance %d status: %v", instance.ID, err)
				continue
			}
			log.Printf("Instance %d expired, container removed", instance.ID)
		}
	}
}
