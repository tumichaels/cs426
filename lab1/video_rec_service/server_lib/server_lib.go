package server_lib

import (
	"context"
	"log"
	"slices"
	"time"

	"sync"
	"sync/atomic"

	"cs426.yale.edu/lab1/ranker"
	umc "cs426.yale.edu/lab1/user_service/mock_client"
	upb "cs426.yale.edu/lab1/user_service/proto"
	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	vpb "cs426.yale.edu/lab1/video_service/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type VideoRecServiceOptions struct {
	// Server address for the UserService"
	UserServiceAddr string
	// Server address for the VideoService
	VideoServiceAddr string
	// Maximum size of batches sent to UserService and VideoService
	MaxBatchSize int
	// Number of clients connections for UserService and VideoService
	ClientPoolSize int
	// If set, disable fallback to cache
	DisableFallback bool
	// If set, disable all retries
	DisableRetry bool
}

type VideoRecServiceServer struct {
	pb.UnimplementedVideoRecServiceServer
	options VideoRecServiceOptions
	// Add any data you want here
	userServiceClient  upb.UserServiceClient
	videoServiceClient vpb.VideoServiceClient

	totalRequests      atomic.Uint64
	totalErrors        atomic.Uint64
	activeRequests     atomic.Uint64
	userServiceErrors  atomic.Uint64
	videoServiceErrors atomic.Uint64
	totalTimeMs        atomic.Uint64
	staleResponses     atomic.Uint64

	topVideos     []*vpb.VideoInfo
	topVideosLock sync.RWMutex
}

func MakeVideoRecServiceServer(options VideoRecServiceOptions) (*VideoRecServiceServer, error) {

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	// Get UserService connection
	userServiceConn, err := grpc.NewClient(options.UserServiceAddr, opts...)
	if err != nil && !options.DisableRetry {
		time.Sleep(time.Duration(10) * time.Millisecond)
		userServiceConn, err = grpc.NewClient(options.UserServiceAddr, opts...)
	}
	if err != nil {
		return nil, status.Errorf(
			codes.Unavailable,
			"VideoRecService: could not connect to UserService: %v",
			err,
		)
	}

	// Get VideosService connection
	videoServiceConn, err := grpc.NewClient(options.VideoServiceAddr, opts...)
	if err != nil && !options.DisableRetry {
		time.Sleep(time.Duration(10) * time.Millisecond)
		userServiceConn, err = grpc.NewClient(options.VideoServiceAddr, opts...)
	}
	if err != nil {
		return nil, status.Errorf(
			codes.Unavailable,
			"VideoRecService: could not connect to VideoService: %v",
			err,
		)
	}

	userServiceClient := upb.NewUserServiceClient(userServiceConn)
	videoServiceClient := vpb.NewVideoServiceClient(videoServiceConn)

	return &VideoRecServiceServer{
		options: options,
		// Add any data to initialize here
		userServiceClient:  userServiceClient,
		videoServiceClient: videoServiceClient,
	}, nil
}

func MakeVideoRecServiceServerWithMocks(
	options VideoRecServiceOptions,
	mockUserServiceClient *umc.MockUserServiceClient,
	mockVideoServiceClient *vmc.MockVideoServiceClient,
) *VideoRecServiceServer {
	// Implement your own logic here
	return &VideoRecServiceServer{
		options: options,
		// ...
		userServiceClient:  mockUserServiceClient,
		videoServiceClient: mockVideoServiceClient,
	}
}

func (server *VideoRecServiceServer) GetTopVideos(
	ctx context.Context,
	req *pb.GetTopVideosRequest,
) (*pb.GetTopVideosResponse, error) {

	server.totalRequests.Add(1)
	server.activeRequests.Add(1)
	defer server.activeRequests.Add(^uint64(0)) // https://github.com/golang/go/issues/39553
	start := time.Now()
	defer func() {
		elapsed := uint64(time.Since(start).Milliseconds())
		server.totalTimeMs.Add(elapsed)
	}()

	// need to validate req
	if req.Limit < 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"VideoRecService: top videos limit must be nonnegative",
		)
	}

	userClient := server.userServiceClient

	// query for user with retry
	userResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{req.UserId}})
	if err != nil && !server.options.DisableRetry {
		time.Sleep(time.Duration(10) * time.Millisecond)
		log.Println("retry initial user query")
		userResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: []uint64{req.UserId}})
	}
	if err != nil {
		log.Println("failed - retry initial user query")
		server.totalErrors.Add(1)
		server.userServiceErrors.Add(1)
		st := status.Convert(err)
		return nil, status.Errorf(
			st.Code(),
			"VideoRecService: error querying UserService for user %d: %v",
			req.UserId,
			st.Message(),
		)
	}

	// extract users which request target is subscribed to
	subscribedTo := userResponse.GetUsers()[0].SubscribedTo

	// get liked videos of subscribed to
	var videosToRank []uint64
	// query for subscribed to
	for i := 0; i < len(subscribedTo); i += server.options.MaxBatchSize {
		end := i + server.options.MaxBatchSize
		if end > len(subscribedTo) {
			end = len(subscribedTo)
		}
		userIds := subscribedTo[i:end]

		// invoke method with retry
		subscribedToResponse, err := userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: userIds})
		if err != nil {
			log.Printf("retry userservice query %d\n", i)
			time.Sleep(time.Duration(10) * time.Millisecond)
			subscribedToResponse, err = userClient.GetUser(ctx, &upb.GetUserRequest{UserIds: userIds})
		}
		if err != nil {
			log.Printf("failed - retry userservice query %d\n", i)
			server.totalErrors.Add(1)
			server.userServiceErrors.Add(1)
			st := status.Convert(err)
			return nil, status.Errorf(
				st.Code(),
				"VideoRecService: error querying UserService for users that user %d is subscribed to: %v",
				req.UserId,
				st.Message(),
			)
		}
		userInfos := subscribedToResponse.GetUsers()
		for _, userInfo := range userInfos {
			videosToRank = append(videosToRank, userInfo.GetLikedVideos()...)
		}
	}
	videosToRank = deduplicateIds(videosToRank)

	// connect to video service and create video service client
	videoClient := server.videoServiceClient

	// query for video info about videos to rank
	// can't do it all in 1 shot ig, so
	var videoInfos []*vpb.VideoInfo
	isStaleResponse := false
	for i := 0; i < len(videosToRank); i += server.options.MaxBatchSize {
		// get batch of accesses
		end := i + server.options.MaxBatchSize
		if end > len(videosToRank) {
			end = len(videosToRank)
		}
		videoIds := videosToRank[i:end]

		// invoke getVideo with retry
		videosToRankResponse, err := videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: videoIds})
		if err != nil && !server.options.DisableRetry {
			time.Sleep(time.Duration(10) * time.Millisecond)
			videosToRankResponse, err = videoClient.GetVideo(ctx, &vpb.GetVideoRequest{VideoIds: videoIds})
		}
		if err != nil {
			server.totalErrors.Add(1)
			server.videoServiceErrors.Add(1)

			// use fallback instead
			if server.options.DisableFallback {
				st := status.Convert(err)
				return nil, status.Errorf(
					st.Code(),
					"VideoRecService: error querying VideoService: %v",
					st.Message(),
				)
			} else {
				server.staleResponses.Add(1)
				isStaleResponse = true
				server.topVideosLock.RLock()
				videoInfos = server.topVideos
				server.topVideosLock.RUnlock()

				if len(videoInfos) == 0 {
					st := status.Convert(err)
					return nil, status.Errorf(
						st.Code(),
						"VideoRecService: error querying VideoService, no cached videos: %v",
						st.Message(),
					)
				}
				break
			}
		}
		videoInfos = append(videoInfos, videosToRankResponse.GetVideos()...)
	}

	// get video ranks
	userCoefficients := userResponse.GetUsers()[0].UserCoefficients
	// videoInfos := videosToRankResponse.GetVideos()
	rankMap := getVideoRanks(userCoefficients, videoInfos)

	// sort videos in descending order by rank
	slices.SortFunc(videoInfos, func(a, b *vpb.VideoInfo) int {
		if rankMap[a] > rankMap[b] {
			return -1
		} else if rankMap[a] == rankMap[b] {
			return 0
		} else {
			return 1
		}
	})

	// select at most limit videos
	if req.Limit != 0 && int(req.Limit) < len(videoInfos) {
		videoInfos = videoInfos[:req.Limit]
	}

	return &pb.GetTopVideosResponse{Videos: videoInfos, StaleResponse: isStaleResponse}, nil
}

func (server *VideoRecServiceServer) GetStats(
	ctx context.Context,
	req *pb.GetStatsRequest,
) (*pb.GetStatsResponse, error) {
	return &pb.GetStatsResponse{
		TotalRequests:      server.totalRequests.Load(),
		TotalErrors:        server.totalErrors.Load(),
		ActiveRequests:     server.activeRequests.Load(),
		UserServiceErrors:  server.userServiceErrors.Load(),
		VideoServiceErrors: server.videoServiceErrors.Load(),
		AverageLatencyMs:   float32(server.totalTimeMs.Load()) / float32(server.totalRequests.Load()),
		StaleResponses:     server.staleResponses.Load(),
	}, nil
}

// func connectToService(target string) (*grpc.ClientConn, error) {
// 	return connectToServiceWithRetry(target, 0)
// }

// func invokeMethodWithRetry(
// 	rpcMethod func() (interface{}, error),
// 	numRetries uint64,
// ) (interface{}, error) {
// 	out, err := rpcMethod()
// 	for range numRetries {
// 		if err == nil {
// 			break
// 		}
// 		out, err = rpcMethod()
// 	}
// 	return out, err
// }

func (server *VideoRecServiceServer) ContinuallyRefreshCache() {
	if server.options.DisableFallback {
		return
	}
	// how long to try reconnecting?
	for {
		videoClient := server.videoServiceClient

		// Query for Trending Videos Ids
		topVideoIdsResponse, err := videoClient.GetTrendingVideos(context.Background(), &vpb.GetTrendingVideosRequest{})
		if err != nil {
			time.Sleep(time.Duration(10) * time.Second)
			continue
		}
		topVideoIds := topVideoIdsResponse.GetVideos()
		expirationTimeSec := int64(topVideoIdsResponse.GetExpirationTimeS())

		// Query for Trending Video Infos
		topVideoInfoResponse, err := videoClient.GetVideo(context.Background(), &vpb.GetVideoRequest{VideoIds: topVideoIds})
		if err != nil {
			time.Sleep(time.Duration(10) * time.Second)
			continue
		}
		topVideoInfo := topVideoInfoResponse.GetVideos()

		// update top videos: need to lock because other threads may be reading
		server.topVideosLock.Lock()
		server.topVideos = topVideoInfo
		server.topVideosLock.Unlock()

		// sleep until next update time
		log.Printf("cache refreshed")
		time.Sleep(time.Until(time.Unix(expirationTimeSec, 0)))
	}
}

func deduplicateIds(ids []uint64) []uint64 {
	deduped := make([]uint64, 0)
	set := make(map[uint64]struct{}, 0)
	for _, id := range ids {
		if _, in_set := set[id]; !in_set {
			set[id] = struct{}{}
			deduped = append(deduped, id)
		}
	}
	return deduped
}

func getVideoRanks(
	coeffs *upb.UserCoefficients,
	videos []*vpb.VideoInfo,
) map[*vpb.VideoInfo]uint64 {

	var videoRanker ranker.BcryptRanker
	rankMap := make(map[*vpb.VideoInfo]uint64)

	for _, videoInfo := range videos {
		rankMap[videoInfo] = videoRanker.Rank(coeffs, videoInfo.GetVideoCoefficients())
	}

	return rankMap
}
