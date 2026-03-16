package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"ququchat/agent/internal/api/grpcserver"
	"ququchat/agent/internal/app"
	"ququchat/agent/internal/config"
	agentservice "ququchat/agent/internal/service/agent"
	grpcpb "ququchat/agent/pkg/grpcpb"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("load agent config failed: %v", err)
	}
	settings := cfg.ToSettings()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closeLLMRuntime, err := app.StartLLMRuntime(ctx, settings)
	if err != nil {
		log.Fatalf("start llm runtime failed: %v", err)
	}
	defer func() {
		if err := closeLLMRuntime(); err != nil {
			log.Printf("close llm runtime failed: %v", err)
		}
	}()

	service := agentservice.NewService()

	lis, err := net.Listen("tcp", settings.Addr)
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	grpcpb.RegisterAgentServiceServer(grpcServer, grpcserver.NewServer(service))

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stopCh
		cancel()
		grpcServer.GracefulStop()
	}()

	log.Printf("agent gRPC server listening on %s", settings.Addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("grpc server stopped: %v", err)
	}
}
