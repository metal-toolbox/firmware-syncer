package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/bmc-toolbox/common"
	"github.com/google/uuid"
	cptypes "github.com/metal-toolbox/conditionorc/pkg/types"
	"github.com/metal-toolbox/firmware-syncer/internal/metrics"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/dell"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

const (
	// conditionInprogressTicker is the interval at which condtion in progress
	// will ack themselves as in progress on the event stream.
	//
	// This value should be set to less than the event stream Ack timeout value.
	conditionInprogressTick = 3 * time.Minute
)

func (s *Syncer) processEvents(ctx context.Context) {
	// XXX: consider having a separate context for message retrieval
	msgs, err := s.stream.PullMsg(ctx, 1)

	switch {
	case err == nil:
	case errors.Is(err, nats.ErrTimeout):
		s.logger.WithFields(
			logrus.Fields{"err": err.Error()},
		).Trace("no new events")
	default:
		s.logger.WithFields(
			logrus.Fields{"err": err.Error()},
		).Warn("retrieving new messages")
		metrics.NATSError("pull-msg")
	}

	for _, msg := range msgs {
		if ctx.Err() != nil || s.concurrencyLimit() {
			s.eventNak(msg)

			return
		}

		// spawn msg process handler
		s.syncWG.Add(1)

		go func(msg events.Message) {
			defer s.syncWG.Done()

			atomic.AddInt32(&s.dispatched, 1)
			defer atomic.AddInt32(&s.dispatched, -1)

			s.processSingleEvent(ctx, msg)
		}(msg)
	}
}

func (s *Syncer) concurrencyLimit() bool {
	return int(s.dispatched) >= s.concurrency
}

func (s *Syncer) eventAckInProgress(event events.Message) {
	if err := event.InProgress(); err != nil {
		metrics.NATSError("ack-in-progress")
		s.logger.WithError(err).Warn("event Ack Inprogress error")
	}
}

func (s *Syncer) eventAckComplete(event events.Message) {
	if err := event.Ack(); err != nil {
		s.logger.WithError(err).Warn("event Ack error")
	}
}

func (s *Syncer) eventNak(event events.Message) {
	if err := event.Nak(); err != nil {
		metrics.NATSError("nak")
		s.logger.WithError(err).Warn("event Nak error")
	}
}

func (s *Syncer) registerEventCounter(valid bool, response string) {
	metrics.EventsCounter.With(
		prometheus.Labels{
			"valid":    strconv.FormatBool(valid),
			"response": response,
		}).Inc()
}

func (s *Syncer) processSingleEvent(ctx context.Context, e events.Message) {
	// extract parent trace context from the event if any.
	ctx = e.ExtractOtelTraceContext(ctx)

	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"syncer.processSingleEvent",
	)
	defer span.End()

	// parse condition from event
	condition, err := conditionFromEvent(e)
	if err != nil {
		s.logger.WithError(err).WithField(
			"subject", e.Subject()).Warn("unable to retrieve condition from message")

		s.registerEventCounter(false, "ack")
		s.eventAckComplete(e)

		return
	}

	// parse parameters from condition
	params, err := s.parametersFromCondition(condition)
	if err != nil {
		s.logger.WithError(err).WithField(
			"subject", e.Subject()).Warn("unable to retrieve parameters from condition")

		s.registerEventCounter(false, "ack")
		s.eventAckComplete(e)

		return
	}

	// fetch firmware from store
	fw, err := s.store.FirmwareByID(ctx, params.FirmwareID)
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"firmwareID":  params.FirmwareID.String(),
			"conditionID": condition.ID,
			"err":         err.Error(),
		}).Warn("firmware lookup error")

		s.registerEventCounter(true, "nack")
		s.eventNak(e) // have the message bus re-deliver the message
		metrics.RegisterSpanEvent(
			span,
			condition,
			s.id.String(),
			params.FirmwareID.String(),
			"sent nack, store query error",
		)

		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, conditionTimeout)
	defer cancel()

	defer s.registerEventCounter(true, "ack")
	defer s.eventAckComplete(e)
	metrics.RegisterSpanEvent(
		span,
		condition,
		s.id.String(),
		params.FirmwareID.String(),
		"sent ack, condition fulfilled",
	)

	s.syncFirmwareWithMonitor(syncCtx, fw, condition.ID, params, e)
}

func conditionFromEvent(e events.Message) (*cptypes.Condition, error) {
	data := e.Data()
	if data == nil {
		return nil, errors.New("data field empty")
	}

	condition := &cptypes.Condition{}
	if err := json.Unmarshal(data, condition); err != nil {
		return nil, errors.Wrap(errConditionDeserialize, err.Error())
	}

	return condition, nil
}

func (s *Syncer) parametersFromCondition(condition *cptypes.Condition) (*types.Parameters, error) {
	errParameters := errors.New("condition parameters error")

	parameters := &types.Parameters{}
	if err := json.Unmarshal(condition.Parameters, parameters); err != nil {
		return nil, errors.Wrap(errParameters, err.Error())
	}

	if s.faultInjection && condition.Fault != nil {
		parameters.Fault = condition.Fault
	}

	return parameters, nil
}

func (s *Syncer) syncFirmwareWithMonitor(ctx context.Context, fw *serverservice.ComponentFirmwareVersion, conditionID uuid.UUID, params *types.Parameters, e events.Message) {
	// the runTask method is expected to close this channel to indicate its done
	doneCh := make(chan bool)

	// monitor sends in progress ack's until the firmware syncer method returns.
	monitor := func() {
		defer s.syncWG.Done()

		ticker := time.NewTicker(conditionInprogressTick)
		defer ticker.Stop()

	Loop:
		for {
			select {
			case <-ticker.C:
				s.eventAckInProgress(e)
			case <-doneCh:
				break Loop
			}
		}
	}

	s.syncWG.Add(1)

	go monitor()

	s.syncFirmware(ctx, fw, conditionID, params, doneCh)

	<-doneCh
}

// where the syncing actually happens
func (s *Syncer) syncFirmware(ctx context.Context, fw *serverservice.ComponentFirmwareVersion, conditionID uuid.UUID, params *types.Parameters, doneCh chan bool) {
	defer close(doneCh)

	startTS := time.Now()

	s.logger.WithFields(logrus.Fields{
		"firmwareID":  fw.UUID.String(),
		"conditionID": conditionID,
	}).Info("actioning condition for firmware")

	s.publishStatus(
		ctx,
		params.FirmwareID.String(),
		cptypes.Active,
		"actioning condition for firmware",
	)

	if params.Fault != nil {
		if params.Fault.FailAt != "" {
			s.publishStatus(
				ctx,
				params.FirmwareID.String(),
				cptypes.Failed,
				"failed due to induced fault",
			)

			s.registerConditionMetrics(startTS, string(cptypes.Failed))
		}

		if params.Fault.Panic {
			panic("fault induced panic")
		}

		d, err := time.ParseDuration(params.Fault.DelayDuration)
		if err == nil {
			time.Sleep(d)
		}
	}

	// XXX Fix this to actually sync firmware
	// Initialize the provider
	v, err := s.initVendor(ctx, fw)
	v.Sync(ctx)

	// call sync method to copy it over

	s.registerConditionMetrics(startTS, string(cptypes.Succeeded))

	s.logger.WithFields(logrus.Fields{
		"firmwareID":  fw.UUID.String(),
		"conditionID": conditionID,
	}).Info("condition for firmware was fulfilled")
}

func (s *Syncer) initVendor(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) (vendors.Vendor, error) {
	switch fw.Vendor {
	case common.VendorDell:
		var dup vendors.Vendor

		fws := []*serverservice.ComponentFirmwareVersion{fw}
		dup, err := dell.NewDUP(context.TODO(), fws, s.logger)
		if err != nil {
			s.logger.Error("Failed to initialize Dell vendor: " + err.Error())
			return nil, err
		}
		return dup, nil
	default:
		s.logger.Error("Vendor not supported: " + fw.Vendor)
	}
}

func (s *Syncer) publishStatus(ctx context.Context, firmwareID string, state cptypes.ConditionState, status string) []byte {
	sv := &types.StatusValue{
		WorkerID: s.id.String(),
		Target:   firmwareID,
		TraceID:  trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
		SpanID:   trace.SpanFromContext(ctx).SpanContext().SpanID().String(),
		State:    string(state),
		Status:   statusInfoJSON(status),
		// ResourceVersion:  XXX: the handler context has no concept of this! does this make
		// sense at the controller-level?
		UpdatedAt: time.Now(),
	}

	return sv.MustBytes()
}

func statusInfoJSON(s string) json.RawMessage {
	return []byte(fmt.Sprintf("{%q: %q}", "msg", s))
}

func (s *Syncer) registerConditionMetrics(startTS time.Time, state string) {
	metrics.ConditionRunTimeSummary.With(
		prometheus.Labels{
			// FIXME: fix this when FirmwareSync is added to conditionorc
			// "condition": string(cptypes.FirmwareSync),
			"state": state,
		},
	).Observe(time.Since(startTS).Seconds())
}
