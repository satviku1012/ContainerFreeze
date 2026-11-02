package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	pb "github.com/podfreeze/core/api/proto/podfreeze"
)

// Server exposes checkpoint and restore operations to the proxy.
type Server struct {
	pb.UnimplementedOrchestratorServer
	Manager *Manager
	Logger  *slog.Logger
}

func (s *Server) CheckpointContainer(ctx context.Context, req *pb.CheckpointRequest) (*pb.OperationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "checkpoint request is nil")
	}
	if s.Manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "container manager is not configured")
	}
	if err := s.Manager.CheckpointContainer(ctx, req.ContainerId, int(req.Pid)); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "checkpoint container: %v", err)
	}
	return &pb.OperationResponse{Success: true, Message: "container checkpointed"}, nil
}

func (s *Server) WakeContainer(ctx context.Context, req *pb.WakeRequest) (*pb.WakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "wake request is nil")
	}
	if s.Manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "container manager is not configured")
	}
	start := time.Now()
	if s.Logger != nil {
		s.Logger.Info("wake request received", "container_id", req.ContainerId, "target_ip", req.TargetIp)
	}
	if err := s.Manager.RestoreContainer(ctx, req.ContainerId); err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return &pb.WakeResponse{Success: false, Message: err.Error()}, nil
	}
	return &pb.WakeResponse{Success: true, Message: "container restored", ReadyTimeMs: int32(time.Since(start).Milliseconds())}, nil
}

func ListenAndServe(addr, criuPath, checkpointDir string, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	manager := NewManager(logger, criuPath, checkpointDir)
	pb.RegisterOrchestratorServer(grpcServer, &Server{Manager: manager, Logger: logger})
	reflection.Register(grpcServer)

	if logger != nil {
		logger.Info("grpc daemon listening", "addr", addr, "checkpoint_dir", checkpointDir, "criu", criuPath)
	}
	return grpcServer.Serve(listener)
}
