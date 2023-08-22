package syncer

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/registry"

	"github.com/metal-toolbox/firmware-syncer/internal/store"
)

const (
	pkgName = "internal/worker"
)

var (
	fetchEventsInterval = 10 * time.Second

	// conditionTimeout defines the time after which the condition execution will be canceled.
	conditionTimeout = 180 * time.Minute

	errConditionDeserialize = errors.New("unable to deserialize condition")
)

type Syncer struct {
	stream            events.Stream
	store             store.Repository
	syncWG            *sync.WaitGroup
	logger            *logrus.Logger
	name              string
	id                registry.ControllerID // assigned when this worker registers itself
	facilityCode      string
	concurrency       int
	dispatched        int32
	dryrun            bool
	faultInjection    bool
	useStatusKV       bool
	replicaCount      int
	statusKVPublisher *statusKVPublisher
}

func New(
	facilityCode string,
	dryrun,
	useStatusKV,
	faultInjection bool,
	concurrency,
	replicaCount int,
	stream events.Stream,
	repository store.Repository,
	logger *logrus.Logger,
) *Syncer {
	id, _ := os.Hostname()

	return &Syncer{
		stream:         stream,
		store:          repository,
		syncWG:         &sync.WaitGroup{},
		logger:         logger,
		name:           id,
		facilityCode:   facilityCode,
		concurrency:    concurrency,
		dryrun:         dryrun,
		faultInjection: faultInjection,
		useStatusKV:    useStatusKV,
		replicaCount:   replicaCount,
	}
}

// Run runs the firmware-syncer worker which listens for events to sync firmware
func (s *Syncer) Run(ctx context.Context) {
	tickerFetchEvents := time.NewTicker(fetchEventsInterval).C

	if err := s.stream.Open(); err != nil {
		s.logger.WithError(err).Error("event stream connection error")
		return
	}

	// returned channel ignored, since this is a Pull based subscription.
	_, err := s.stream.Subscribe(ctx)
	if err != nil {
		s.logger.WithError(err).Error("event stream subscription error")
		return
	}

	s.logger.Info("connected to event stream.")

	s.startSyncerLivenessCheckin(ctx)

	s.statusKVPublisher = newStatusKVPublisher(s.stream, s.replicaCount, s.logger)

	s.logger.WithFields(
		logrus.Fields{
			"replica-count":   s.replicaCount,
			"concurrency":     s.concurrency,
			"dry-run":         s.dryrun,
			"fault-injection": s.faultInjection,
		},
	).Info("firmware-syncer running")

Loop:
	for {
		select {
		case <-tickerFetchEvents:
			if s.concurrencyLimit() {
				continue
			}

			s.processEvents(ctx)

		case <-ctx.Done():
			if s.dispatched > 0 {
				continue
			}

			break Loop
		}
	}
}
