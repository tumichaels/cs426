package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	logging "cs426.yale.edu/lab1/logging"
	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	sl "cs426.yale.edu/lab1/video_rec_service/server_lib"
	"google.golang.org/grpc"
)

var (
	port            = flag.Int("port", 8080, "The server port")
	userServiceAddr = flag.String(
		"user-service",
		"[::]:8081",
		"Server address for the UserService",
	)
	videoServiceAddr = flag.String(
		"video-service",
		"[::]:8082",
		"Server address for the VideoService",
	)
	clientPoolSize = flag.Int(
		"client-pool-size",
		4,
		"Number of connections for each service",
	)
	maxBatchSize = flag.Int(
		"batch-size",
		50,
		"Maximum size of batches sent to UserService and VideoService",
	)
	disableFallback = flag.Bool(
		"no-fallback",
		false,
		"If set, disable fallback to cache",
	)
	disableRetry = flag.Bool(
		"no-retry",
		false,
		"If set, disable all retries",
	)
)

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("max batch size: %d\n", *maxBatchSize)
	s := grpc.NewServer(grpc.UnaryInterceptor(logging.MakeMiddleware(logging.MakeLogger())))
	server, err := sl.MakeVideoRecServiceServer(sl.VideoRecServiceOptions{
		UserServiceAddr:  *userServiceAddr,
		VideoServiceAddr: *videoServiceAddr,
		MaxBatchSize:     *maxBatchSize,
		DisableFallback:  *disableFallback,
		DisableRetry:     *disableRetry,
		ClientPoolSize:   *clientPoolSize,
	})
	if err != nil {
		log.Fatalf("failed to start server: %q", err)
	}
	go server.ContinuallyRefreshCache()
	pb.RegisterVideoRecServiceServer(s, server)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
