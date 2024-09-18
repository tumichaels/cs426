package lab0_test

import (
	"context"
	"testing"
	"time"

	"cs426.cloud/lab0"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func chanToSlice[T any](ch chan T) []T {
	vals := make([]T, 0)
	for item := range ch {
		vals = append(vals, item)
	}
	return vals
}

type mergeFunc = func(chan string, chan string, chan string)

func runMergeTest(t *testing.T, merge mergeFunc) {
	t.Run("empty channels", func(t *testing.T) {
		a := make(chan string)
		b := make(chan string)
		out := make(chan string)
		close(a)
		close(b)

		merge(a, b, out)
		// If your lab0 hangs here, make sure you are closing your channels!
		require.Empty(t, chanToSlice(out))
	})

	t.Run("1 full channel", func(t *testing.T) {
		a := make(chan string)
		b := make(chan string)
		out := make(chan string)
		go merge(a, b, out)
		go func() {
			a <- "a1"
			a <- "a2"
			close(a)
			close(b)
		}()
		// If your lab0 hangs here, make sure you are closing your channels!
		require.ElementsMatch(t, []string{"a1", "a2"}, chanToSlice(out))
	})

	// Please write your own tests
	t.Run("2 full channels", func(t *testing.T) {
		a := make(chan string)
		b := make(chan string)
		out := make(chan string)
		go merge(a, b, out)
		go func() {
			a <- "a1"
			a <- "a2"
			close(a)
		}()
		go func() {
			b <- "b1"
			b <- "b2"
			close(b)
		}()
		// If your lab0 hangs here, make sure you are closing your channels!
		require.ElementsMatch(t, []string{"a1", "a2", "b1", "b2"}, chanToSlice(out))
	})
}

func TestMergeChannels(t *testing.T) {
	runMergeTest(t, func(a, b, out chan string) {
		lab0.MergeChannels(a, b, out)
	})
}

func TestMergeOrCancel(t *testing.T) {
	runMergeTest(t, func(a, b, out chan string) {
		_ = lab0.MergeChannelsOrCancel(context.Background(), a, b, out)
	})

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		a := make(chan string, 1)
		b := make(chan string, 1)
		out := make(chan string, 10)

		eg, _ := errgroup.WithContext(context.Background())
		eg.Go(func() error {
			return lab0.MergeChannelsOrCancel(ctx, a, b, out)
		})
		err := eg.Wait()
		a <- "a"
		b <- "b"

		require.Error(t, err)
		require.Equal(t, []string{}, chanToSlice(out))
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		a := make(chan string)
		b := make(chan string)
		out := make(chan string, 10)

		eg, _ := errgroup.WithContext(context.Background())
		eg.Go(func() error {
			return lab0.MergeChannelsOrCancel(ctx, a, b, out)
		})
		a <- "a"
		b <- "b"
		cancel()

		err := eg.Wait()
		require.Error(t, err)
		require.Equal(t, []string{"a", "b"}, chanToSlice(out))
	})
}

type channelFetcher struct {
	ch chan string
}

type slowChannelFetcher struct {
	ch chan string
}

func newChannelFetcher(ch chan string) *channelFetcher {
	return &channelFetcher{ch: ch}
}

func (f *channelFetcher) Fetch() (string, bool) {
	v, ok := <-f.ch
	return v, ok
}

func newSlowChannelFetcher(ch chan string) *slowChannelFetcher {
	return &slowChannelFetcher{ch: ch}
}

func (f *slowChannelFetcher) Fetch() (string, bool) {
	time.Sleep(time.Duration(2) * time.Second)
	v, ok := <-f.ch
	return v, ok
}

func TestMergeFetches(t *testing.T) {
	runMergeTest(t, func(a, b, out chan string) {
		lab0.MergeFetches(newChannelFetcher(a), newChannelFetcher(b), out)
	})
}

func TestMergeFetchesAdditional(t *testing.T) {
	// TODO: add your extra tests here
	runMergeTest(t, func(a, b, out chan string) {
		lab0.MergeFetches(newSlowChannelFetcher(a), newChannelFetcher(b), out)
	})
	runMergeTest(t, func(a, b, out chan string) {
		lab0.MergeFetches(newSlowChannelFetcher(a), newSlowChannelFetcher(b), out)
	})
}
