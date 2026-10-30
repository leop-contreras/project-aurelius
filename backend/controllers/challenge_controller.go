package controllers

import (
	"context"
	"main/proto_gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *ReadController) ListChallenges(ctx context.Context, req *proto_gen.ListChallengesRequest) (*proto_gen.ChallengeList, error) {
	challenges, err := c.ChallengeModel.ListChallenges()
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to list challenges")
	}

	list := &proto_gen.ChallengeList{}
	for _, challenge := range challenges {
		list.Challenges = append(list.Challenges, &proto_gen.Challenge{
			Id:          int32(challenge.ID),
			Name:        challenge.Name,
			Description: challenge.Description,
		})
	}

	setWorkerID(ctx)
	return list, nil
}
