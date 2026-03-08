// Package workflow — event streaming for real-time workflow monitoring.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// EventType identifies the type of workflow event.
type EventType string

const (
	EventRunStarted    EventType = "run.started"
	EventRunCompleted  EventType = "run.completed"
	EventRunFailed     EventType = "run.failed"
	EventStepStarted   EventType = "step.started"
	EventStepCompleted EventType = "step.completed"
	EventStepFailed    EventType = "step.failed"
	EventStepRetrying  EventType = "step.retrying"
	EventCheckpoint    EventType = "checkpoint.saved"
)

// Event is a workflow lifecycle event emitted to subscribers.
type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	RunID     string         `json:"runId"`
	StepKey   string         `json:"stepKey,omitempty"`
	ProjectID string         `json:"projectId"`
	Data      map[string]any `json:"data,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// EventHandler processes workflow events.
type EventHandler func(event Event)

// EventStream manages event subscriptions and publishing.
type EventStream struct {
	handlers []EventHandler
	persist  func(ctx context.Context, event Event) error
}

// NewEventStream creates a new event stream.
func NewEventStream() *EventStream {
	return &EventStream{}
}

// Subscribe registers a handler for workflow events.
func (es *EventStream) Subscribe(handler EventHandler) {
	es.handlers = append(es.handlers, handler)
}

// SetPersistence enables event persistence to PostgreSQL.
func (es *EventStream) SetPersistence(fn func(ctx context.Context, event Event) error) {
	es.persist = fn
}

// Emit publishes an event to all subscribers.
func (es *EventStream) Emit(ctx context.Context, event Event) {
	event.Timestamp = time.Now()

	for _, h := range es.handlers {
		h(event)
	}

	// Persist if configured
	if es.persist != nil {
		_ = es.persist(ctx, event)
	}
}

// EmitStepStarted emits a step.started event.
func (es *EventStream) EmitStepStarted(ctx context.Context, runID, projectID, stepKey string) {
	es.Emit(ctx, Event{
		ID:        fmt.Sprintf("%s-%s-started", runID, stepKey),
		Type:      EventStepStarted,
		RunID:     runID,
		StepKey:   stepKey,
		ProjectID: projectID,
	})
}

// EmitStepCompleted emits a step.completed event with output summary.
func (es *EventStream) EmitStepCompleted(ctx context.Context, runID, projectID, stepKey string, output map[string]any) {
	es.Emit(ctx, Event{
		ID:        fmt.Sprintf("%s-%s-completed", runID, stepKey),
		Type:      EventStepCompleted,
		RunID:     runID,
		StepKey:   stepKey,
		ProjectID: projectID,
		Data:      output,
	})
}

// EmitStepFailed emits a step.failed event.
func (es *EventStream) EmitStepFailed(ctx context.Context, runID, projectID, stepKey string, err error) {
	es.Emit(ctx, Event{
		ID:        fmt.Sprintf("%s-%s-failed", runID, stepKey),
		Type:      EventStepFailed,
		RunID:     runID,
		StepKey:   stepKey,
		ProjectID: projectID,
		Error:     err.Error(),
	})
}

// SSEHandler returns an http.HandlerFunc for Server-Sent Events.
func (es *EventStream) SSEHandler() func(projectID string, w interface{ Write([]byte) (int, error) }, flush func(), done <-chan struct{}) {
	return func(projectID string, w interface{ Write([]byte) (int, error) }, flush func(), done <-chan struct{}) {
		events := make(chan Event, 50)

		es.Subscribe(func(event Event) {
			if event.ProjectID == projectID {
				select {
				case events <- event:
				default:
				}
			}
		})

		for {
			select {
			case <-done:
				return
			case event := <-events:
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
				flush()
			case <-time.After(30 * time.Second):
				fmt.Fprintf(w, ": keepalive\n\n")
				flush()
			}
		}
	}
}

// Ensure EventStream.SSEHandler writer interface satisfaction
type fmtWriter interface {
	Write([]byte) (int, error)
}

var _ fmtWriter = (*fmtWriterImpl)(nil)

type fmtWriterImpl struct{}

func (fmtWriterImpl) Write(p []byte) (int, error) { return len(p), nil }
