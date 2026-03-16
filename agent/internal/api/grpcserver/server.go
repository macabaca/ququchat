package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentservice "ququchat/agent/internal/service/agent"
	grpcpb "ququchat/agent/pkg/grpcpb"
)

type Server struct {
	grpcpb.UnimplementedAgentServiceServer
	service *agentservice.Service
}

func NewServer(service *agentservice.Service) *Server {
	return &Server{service: service}
}

func (s *Server) Add(_ context.Context, req *grpcpb.AddRequest) (*grpcpb.AddResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	return &grpcpb.AddResponse{
		TaskType: grpcpb.TaskType_TASK_TYPE_ADD,
		Sum:      s.service.Add(req.GetA(), req.GetB()),
	}, nil
}
