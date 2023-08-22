package syncer

import (
	"context"
	"fmt"
	"time"

	"github.com/metal-toolbox/firmware-syncer/internal/metrics"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/pkg/kv"
)

var (
	statusKVName  = string(types.ConditionKindFirmwareSync)
	defaultKVOpts = []kv.Option{
		kv.WithDescription("firmware-syncer condition status tracking"),
		kv.WithTTL(10 * 24 * time.Hour),
	}
)

type statusKVPublisher struct {
	facilityCode string
	workerID     string
	kv           nats.KeyValue
	log          *logrus.Logger
}

func newStatusKVPublisher(s events.Stream, replicaCount int, log *logrus.Logger) *statusKVPublisher {
	var opts []kv.Option
	if replicaCount > 1 {
		opts = append(opts, kv.WithReplicas(replicaCount))
	}

	js, ok := s.(*events.NatsJetstream)
	if !ok {
		log.Fatal("status-kv publisher is only supported on NATS")
	}

	kvOpts := defaultKVOpts
	kvOpts = append(kvOpts, opts...)

	statusKV, err := kv.CreateOrBindKVBucket(js, statusKVName, kvOpts...)
	if err != nil {
		log.WithError(err).Fatal("unable to bind status KV bucket")
	}

	return &statusKVPublisher{
		kv:  statusKV,
		log: log,
	}
}

// Publish publishes the condition status
func (s *statusKVPublisher) Publish(ctx context.Context, firmwareID, conditionID string, lastRevision uint64, payload []byte) (revision uint64) {
	_, span := otel.Tracer(pkgName).Start(
		ctx,
		"worker.Publish.KV",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	facility := "facility"

	key := fmt.Sprintf("%s.%s", facility, conditionID)

	var err error
	if lastRevision == 0 {
		revision, err = s.kv.Create(key, payload)
	} else {
		revision, err = s.kv.Update(key, payload, lastRevision)
	}

	if err != nil {
		metrics.NATSError("publish-condition-status")
		span.AddEvent("status publish failure",
			trace.WithAttributes(
				attribute.String("workerID", s.workerID),
				attribute.String("firmwareID", firmwareID),
				attribute.String("conditionID", conditionID),
				attribute.String("error", err.Error()),
			),
		)
		s.log.WithError(err).WithFields(logrus.Fields{
			"workerID":          s.workerID,
			"firmwareID":        firmwareID,
			"assetFacilityCode": s.facilityCode,
			"conditionID":       conditionID,
			"lastRev":           lastRevision,
		}).Warn("unable to write condition status")

		return
	}

	s.log.WithFields(logrus.Fields{
		"workerID":          s.workerID,
		"firmwareID":        firmwareID,
		"assetFacilityCode": s.facilityCode,
		"conditionID":       conditionID,
		"lastRev":           lastRevision,
	}).Trace("published condition status")

	return revision
}