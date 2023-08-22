package syncer

import (
	"context"
	"sync"
	"time"

	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/pkg/kv"
	"go.hollow.sh/toolbox/events/registry"

	"github.com/nats-io/nats.go"
)

var (
	once           sync.Once
	checkinCadence = 30 * time.Second
	livenessTTL    = 3 * time.Minute
)

// This starts a go-routine to periodically check in with the NATS kv
func (s *Syncer) startSyncerLivenessCheckin(ctx context.Context) {
	once.Do(func() {
		s.id = registry.GetID(s.name)
		natsJS, ok := s.stream.(*events.NatsJetstream)
		if !ok {
			s.logger.Error("Non-NATS stores are not supported for worker-liveness")
			return
		}

		opts := []kv.Option{
			kv.WithTTL(livenessTTL),
		}

		// any setting of replicas (even 1) chokes NATS in non-clustered mode
		if s.replicaCount != 1 {
			opts = append(opts, kv.WithReplicas(s.replicaCount))
		}

		if err := registry.InitializeRegistryWithOptions(natsJS, opts...); err != nil {
			s.logger.WithError(err).Error("unable to initialize active worker registry")
			return
		}

		go s.checkinRoutine(ctx)
	})
}

func (s *Syncer) checkinRoutine(ctx context.Context) {
	if err := registry.RegisterController(s.id); err != nil {
		s.logger.WithError(err).Warn("unable to do initial worker liveness registration")
	}

	tick := time.NewTicker(checkinCadence)
	defer tick.Stop()

	var stop bool
	for !stop {
		select {
		case <-tick.C:
			err := registry.ControllerCheckin(s.id)
			switch err {
			case nil:
			case nats.ErrKeyNotFound: // generally means NATS reaped our entry on TTL
				if err = registry.RegisterController(s.id); err != nil {
					s.logger.WithError(err).
						WithField("id", s.id.String()).
						Warn("unable to re-register worker")
				}
			default:
				s.logger.WithError(err).
					WithField("id", s.id.String()).
					Warn("worker checkin failed")
			}
		case <-ctx.Done():
			s.logger.Info("liveness check-in stopping on done context")

			stop = true
		}
	}
}
