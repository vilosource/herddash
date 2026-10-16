package engine

import (
	"context"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

// Client is what the engine needs from herdr. NewHerdrClient provides the real
// one; tests provide a fake, so no engine test touches a live herdr.
type Client interface {
	Ping(ctx context.Context) (string, error)
	Snapshot(ctx context.Context) (core.Snapshot, error)
	SubscribeLifecycle(ctx context.Context) (Subscription[core.LifecycleEvent], error)
	SubscribeStatus(ctx context.Context, paneID string) (Subscription[core.StatusEvent], error)
}

// Subscription is one live subscription, as herdr.Subscription provides it:
// Deliveries yields events in order and at most one health signal last, then
// closes; Close ends it and is safe to call more than once.
type Subscription[T any] interface {
	Deliveries() <-chan herdr.Delivery[T]
	Close() error
}

// NewHerdrClient returns a Client that speaks to the herdr socket at
// socketPath. It does not dial.
func NewHerdrClient(socketPath string) Client {
	return herdrClient{c: herdr.New(socketPath)}
}

type herdrClient struct{ c *herdr.Client }

func (h herdrClient) Ping(ctx context.Context) (string, error) { return h.c.Ping(ctx) }

func (h herdrClient) Snapshot(ctx context.Context) (core.Snapshot, error) { return h.c.Snapshot(ctx) }

func (h herdrClient) SubscribeLifecycle(ctx context.Context) (Subscription[core.LifecycleEvent], error) {
	s, err := h.c.SubscribeLifecycle(ctx)
	if err != nil {
		return nil, err
	}
	return herdrSubscription[core.LifecycleEvent]{s}, nil
}

func (h herdrClient) SubscribeStatus(ctx context.Context, paneID string) (Subscription[core.StatusEvent], error) {
	s, err := h.c.SubscribeStatus(ctx, paneID)
	if err != nil {
		return nil, err
	}
	return herdrSubscription[core.StatusEvent]{s}, nil
}

type herdrSubscription[T any] struct{ s *herdr.Subscription[T] }

func (s herdrSubscription[T]) Deliveries() <-chan herdr.Delivery[T] { return s.s.C }

func (s herdrSubscription[T]) Close() error { return s.s.Close() }
