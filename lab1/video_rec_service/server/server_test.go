package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	fipb "cs426.yale.edu/lab1/failure_injection/proto"
	umc "cs426.yale.edu/lab1/user_service/mock_client"
	usl "cs426.yale.edu/lab1/user_service/server_lib"
	vmc "cs426.yale.edu/lab1/video_service/mock_client"
	vpb "cs426.yale.edu/lab1/video_service/proto"
	vsl "cs426.yale.edu/lab1/video_service/server_lib"

	pb "cs426.yale.edu/lab1/video_rec_service/proto"
	sl "cs426.yale.edu/lab1/video_rec_service/server_lib"
)

func SetUpServers(
	t *testing.T,
	usOptions usl.UserServiceOptions,
	vsOptions vsl.VideoServiceOptions,
	vrOptions sl.VideoRecServiceOptions,
) (uClient *umc.MockUserServiceClient, vClient *vmc.MockVideoServiceClient, vrService *sl.VideoRecServiceServer) {
	uClient = umc.MakeMockUserServiceClient(usOptions)
	vClient = vmc.MakeMockVideoServiceClient(vsOptions)
	vrService = sl.MakeVideoRecServiceServerWithMocks(
		vrOptions,
		uClient,
		vClient,
	)
	return uClient, vClient, vrService
}

func CheckSeed204054Response(t *testing.T, videos []*vpb.VideoInfo) {
	assert.Equal(t, 5, len(videos))
	assert.EqualValues(t, 1012, videos[0].VideoId)
	assert.Equal(t, "Harry Boehm", videos[1].Author)
	assert.EqualValues(t, 1209, videos[2].VideoId)
	assert.Equal(t, "https://video-data.localhost/blob/1309", videos[3].Url)
	assert.Equal(t, "precious here", videos[4].Title)
}

func TestServerBasic(t *testing.T) {
	vrOptions := sl.VideoRecServiceOptions{
		MaxBatchSize:    50,
		DisableFallback: true,
		DisableRetry:    true,
	}
	_, _, vrService := SetUpServers(
		t,
		*usl.DefaultUserServiceOptions(),
		*vsl.DefaultVideoServiceOptions(),
		vrOptions,
	)
	// You can specify failure injection options here or later send
	// SetInjectionConfigRequests using these mock clients

	var userId uint64 = 204054
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := vrService.GetTopVideos(
		ctx,
		&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
	)
	assert.True(t, err == nil)

	videos := out.Videos
	CheckSeed204054Response(t, videos)
}

func TestUserServiceFailure(t *testing.T) {

	t.Run("fail initial UserService query", func(t *testing.T) {
		usOptions := *usl.DefaultUserServiceOptions()
		usOptions.FailureRate = 1

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: true,
			DisableRetry:    true,
		}
		_, _, vrService := SetUpServers(
			t,
			usOptions,
			*vsl.DefaultVideoServiceOptions(),
			vrOptions,
		)

		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.False(t, err == nil)
	})

	t.Run("fail first subscribed-to UserService query", func(t *testing.T) {
		usOptions := *usl.DefaultUserServiceOptions()
		usOptions.FailureRate = 2

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: true,
			DisableRetry:    true,
		}
		_, _, vrService := SetUpServers(
			t,
			usOptions,
			*vsl.DefaultVideoServiceOptions(),
			vrOptions,
		)

		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.False(t, err == nil)
	})

	t.Run("fail subsequent subscribed-to UserService query", func(t *testing.T) {
		usOptions := *usl.DefaultUserServiceOptions()
		usOptions.FailureRate = 3

		// the initial userService request returns serveral subscribed-to
		// users to query about. this should fail on the 3rd query to
		// user service, or, the second query for subscribed-to users
		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    1,
			DisableFallback: true,
			DisableRetry:    true,
		}
		_, _, vrService := SetUpServers(
			t,
			usOptions,
			*vsl.DefaultVideoServiceOptions(),
			vrOptions,
		)

		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.False(t, err == nil)
	})
}

func TestVideoServiceFailure(t *testing.T) {

	t.Run("fail initial VideoService query", func(t *testing.T) {
		vsOptions := *vsl.DefaultVideoServiceOptions()
		vsOptions.FailureRate = 1

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: true,
			DisableRetry:    true,
		}
		_, _, vrService := SetUpServers(
			t,
			*usl.DefaultUserServiceOptions(),
			vsOptions,
			vrOptions,
		)

		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.False(t, err == nil)
	})
}

func TestRetry(t *testing.T) {

	// 33% chance of failure on any request
	// !!not!! as i assumed failing every 3rd request

	t.Run("33%% UserService failure -- with retry", func(t *testing.T) {
		usOptions := *usl.DefaultUserServiceOptions()
		usOptions.FailureRate = 3

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: true,
			DisableRetry:    false,
		}
		_, _, vrService := SetUpServers(
			t,
			usOptions,
			*vsl.DefaultVideoServiceOptions(),
			vrOptions,
		)

		// for range 10 {
		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		out, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.True(t, err == nil)

		// make sure we faced at least one error
		stats, _ := vrService.GetStats(context.Background(), &pb.GetStatsRequest{})
		assert.GreaterOrEqual(t, int(stats.UserServiceErrors), 0)

		videos := out.Videos
		assert.Equal(t, 5, len(videos))
		assert.EqualValues(t, 1012, videos[0].VideoId)
		assert.Equal(t, "Harry Boehm", videos[1].Author)
		assert.EqualValues(t, 1209, videos[2].VideoId)
		assert.Equal(t, "https://video-data.localhost/blob/1309", videos[3].Url)
		assert.Equal(t, "precious here", videos[4].Title)

		// }
	})

	// errors if no fallback
	t.Run("33%% VideoService failure -- with retry", func(t *testing.T) {
		vsOptions := *vsl.DefaultVideoServiceOptions()
		vsOptions.FailureRate = 3

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: true,
			DisableRetry:    false,
		}
		_, _, vrService := SetUpServers(
			t,
			*usl.DefaultUserServiceOptions(),
			vsOptions,
			vrOptions,
		)

		// for range 10 {
		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		assert.False(t, err == nil)

		// make sure we faced at least one error
		stats, _ := vrService.GetStats(context.Background(), &pb.GetStatsRequest{})
		assert.GreaterOrEqual(t, int(stats.VideoServiceErrors), 0)
	})

	t.Run("33%% VideoService failure -- with retry and fallback", func(t *testing.T) {
		vsOptions := *vsl.DefaultVideoServiceOptions()
		vsOptions.FailureRate = 0

		vrOptions := sl.VideoRecServiceOptions{
			MaxBatchSize:    50,
			DisableFallback: false,
			DisableRetry:    false,
		}
		_, vClient, vrService := SetUpServers(
			t,
			*usl.DefaultUserServiceOptions(),
			vsOptions,
			vrOptions,
		)

		var userId uint64 = 204054
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// give a chance to initialize cache
		go vrService.ContinuallyRefreshCache()
		time.Sleep(time.Duration(1) * time.Second)

		// fail videoservice requests
		vsOptions.FailureRate = 1
		vClient.SetInjectionConfig(ctx, &fipb.SetInjectionConfigRequest{
			Config: &fipb.InjectionConfig{
				// fail one in 1 request, i.e., always fail
				FailureRate: 1,
			},
		})

		out, err := vrService.GetTopVideos(
			ctx,
			&pb.GetTopVideosRequest{UserId: userId, Limit: 5},
		)
		t.Log(err)
		assert.True(t, err == nil)

		// make sure we faced at least one error
		stats, _ := vrService.GetStats(context.Background(), &pb.GetStatsRequest{})
		assert.GreaterOrEqual(t, int(stats.VideoServiceErrors), 0)

		// using fallback
		assert.GreaterOrEqual(t, len(out.Videos), 0)
		assert.True(t, out.StaleResponse)
	})
}
