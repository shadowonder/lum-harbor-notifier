// Package assistant owns the Global AI Assistant's read path: it resolves the
// user's UI context against authoritative backend truth, executes one
// canonical assistant.ask request, and validates the typed result envelope.
//
// For MUTATION_INTENT it additionally executes one canonical
// assistant.proposal request and returns a validated, typed candidate bound to
// the authoritative snapshot the model actually saw (see candidate.go).
//
// It deliberately depends only on package ai. It has no WorkItem/Proposal
// mutation dependency, no database handle, and no provider selection: the
// injected Executor is the production AI composition (ai.Service.Execute).
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/readcap"
)

const (
	SurfaceWorkItems     = "WORK_ITEMS"
	SurfacePlanner       = "PLANNER"
	SurfaceDashboard     = "DASHBOARD"
	SurfaceNotifications = "NOTIFICATIONS"
	SurfaceHistory       = "HISTORY"
	SurfaceSettings      = "SETTINGS"

	ResultVersion = 1

	maxMessageRunes = 4000
)

// ResultKind is the validated decision the model made about the user message.
type ResultKind string

const (
	KindAnswer         ResultKind = "ANSWER"
	KindMutationIntent ResultKind = "MUTATION_INTENT"
	KindNeedsContext   ResultKind = "NEEDS_CONTEXT"
	KindUnsupported    ResultKind = "UNSUPPORTED"
)

var ErrRuntime = errors.New("assistant AI execution failed")
var ErrMalformedResult = errors.New("assistant returned a malformed result")
var ErrEmptyMessage = errors.New("assistant message must not be empty")
var ErrMessageTooLong = errors.New("assistant message is too long")
var ErrWorkItemNotFound = errors.New("assistant context work item not found")
var ErrContext = errors.New("assistant context error")

// Executor is the canonical AI execution boundary (ai.Service.Execute via the
// production composition).
type Executor interface {
	Execute(context.Context, ai.Request) (ai.Response, error)
}

// WorkItemSnapshot is the read-only authoritative WorkItem truth the
// Assistant may see. Only fields the backend really owns exist here.
type WorkItemSnapshot struct {
	ID string
	// LastRevisionID is application-side concurrency evidence. It is never
	// sent to the model.
	LastRevisionID string
	Title          string
	Description    *string
	Lifecycle      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
	Tracking       *TrackingSnapshot
}

type TrackingStepSnapshot struct{ ID, Title string }
type TrackedItemSnapshot struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	ItemType         *string `json:"item_type,omitempty"`
	ParentItemID     *string `json:"parent_item_id,omitempty"`
	ParentItemTitle  *string `json:"parent_item_title,omitempty"`
	State            string  `json:"state,omitempty"`
	CurrentStepID    *string `json:"current_step_id,omitempty"`
	CurrentStepTitle *string `json:"current_step_title,omitempty"`
	Complete         bool    `json:"complete,omitempty"`
	LastRevisionID   string  `json:"last_revision_id,omitempty"`
}
type TrackingRelationSnapshot struct{ FromTrackedItemID, ToTrackedItemID string }
type MonitorPointSnapshot struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	DueAt       string  `json:"due_at"`
	Timezone    string  `json:"timezone"`
	SubjectKind string  `json:"subject_kind"`
	SubjectID   string  `json:"subject_id"`
	Expected    *string `json:"expected,omitempty"`
}
type TrackingSnapshot struct {
	WorkflowID    string                     `json:"workflow_id"`
	CurrentStep   *TrackingStepSnapshot      `json:"current_step,omitempty"`
	Steps         []TrackingStepSnapshot     `json:"steps,omitempty"`
	Items         []TrackedItemSnapshot      `json:"items,omitempty"`
	Relations     []TrackingRelationSnapshot `json:"relations,omitempty"`
	MonitorPoints []MonitorPointSnapshot     `json:"monitor_points,omitempty"`
}

// WorkItemReader is the only data access the Assistant has. It is read-only
// by construction; a not-found WorkItem must wrap ErrWorkItemNotFound.
type WorkItemReader interface {
	ReadWorkItem(ctx context.Context, id string) (WorkItemSnapshot, error)
}

// ContextLocator is all the UI may say: which surface, and which WorkItem the
// user currently has selected. Frontend-rendered WorkItem fields are never
// accepted; the ID is re-resolved against authoritative truth. Now allows
// deterministic time overrides in tests; if zero, the current time is used.
type ContextLocator struct {
	Now        time.Time `json:"-"`
	Surface    string
	WorkItemID string
}

// ApplicationContext holds the authoritative application runtime context
// supplied to the high-level Runtime. Current date/time and system timezone
// represent the host application's environment. Surface and WorkItemID are
// optional context hints, never permission gates.
type ApplicationContext struct {
	Now        time.Time
	Surface    string
	WorkItemID string
}

func (c ApplicationContext) FormatAuthoritativeContext(item *WorkItemSnapshot) string {
	now := c.Now
	if now.IsZero() {
		now = time.Now()
	}
	zoneName, _ := now.Zone()
	tz := now.Location().String()
	if tz == "" || tz == "Local" {
		tz = zoneName
	} else if zoneName != "" && zoneName != tz {
		tz = fmt.Sprintf("%s (%s)", tz, zoneName)
	}

	surface := strings.TrimSpace(c.Surface)
	if surface == "" {
		surface = "Global"
	}

	selectedEntity := "none"
	if item != nil {
		selectedEntity = fmt.Sprintf("WorkItem %s (%q)", item.ID, item.Title)
	} else if strings.TrimSpace(c.WorkItemID) != "" {
		selectedEntity = fmt.Sprintf("WorkItem %s", strings.TrimSpace(c.WorkItemID))
	}

	var sb strings.Builder
	sb.WriteString("Authoritative application runtime context:\n")
	fmt.Fprintf(&sb, "- Current local date/time: %s\n", now.Format(time.RFC3339))
	// Relative requests ("next Friday") need the weekday; models derive it
	// from a bare date unreliably.
	fmt.Fprintf(&sb, "- Current local weekday: %s\n", now.Weekday())
	fmt.Fprintf(&sb, "- Local UTC offset: %s\n", now.Format("-07:00"))
	fmt.Fprintf(&sb, "- System timezone: %s\n", tz)
	fmt.Fprintf(&sb, "- Current surface: %s\n", surface)
	fmt.Fprintf(&sb, "- Selected entity: %s\n", selectedEntity)
	return sb.String()
}

// Result is the validated, product-facing outcome of one Ask.
type Result struct {
	Kind ResultKind
	Text string
	// ContextLabel is the authoritative title of the resolved WorkItem, empty
	// when no context was resolved.
	ContextLabel string
	// ChangeApplied is always false: the Assistant never mutates
	// authoritative state. Producing a candidate is not applying a change.
	ChangeApplied bool
	// Candidate is set only for MUTATION_INTENT when the model produced a
	// valid candidate that differs from Base. The application, not this
	// package, decides whether to turn it into a Proposal.
	Candidate *MutationCandidate
	// Plan is set instead of Candidate for MUTATION_INTENT when the model
	// proposed one Proposal of several ordered operations.
	Plan *MutationPlan
	// Base is the authoritative snapshot the candidate was derived from.
	Base *WorkItemSnapshot
	// NoChange is true when a valid candidate equals Base.
	NoChange bool
	// ReadRequest is the executed read capability request, if any.
	ReadRequest *readcap.Request
	// ReadResult is the result of the executed read capability, if any.
	ReadResult *readcap.Result
	// Evidence holds all executed read evidence.
	Evidence []ReadEvidence
	// Provenance is the real ai.Service provenance of the final model call,
	// recorded on the Assistant's durable AI Run.
	Provenance ai.ResponseProvenance
}

type Service struct {
	reader  WorkItemReader
	runtime *RuntimeAdapter
}

func NewService(reader WorkItemReader, runtime *RuntimeAdapter) (*Service, error) {
	if reader == nil || runtime == nil {
		return nil, errors.New("assistant requires a work item reader and AI runtime adapter")
	}
	return &Service{reader: reader, runtime: runtime}, nil
}

// Ask resolves context, executes the high-level runtime, and maps the result.
func (s *Service) Ask(ctx context.Context, requestID, message string, locator ContextLocator) (Result, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Result{}, ErrEmptyMessage
	}
	if len([]rune(message)) > maxMessageRunes {
		return Result{}, ErrMessageTooLong
	}

	var itemPtr *WorkItemSnapshot
	var contextLabel string

	trimmedID := strings.TrimSpace(locator.WorkItemID)
	if trimmedID != "" {
		item, err := s.reader.ReadWorkItem(ctx, trimmedID)
		if err != nil {
			if errors.Is(err, ErrWorkItemNotFound) {
				return Result{
					Kind: KindNeedsContext,
					Text: "The selected Work Item no longer exists. Select an existing Work Item and try again.",
				}, nil
			}
			return Result{}, fmt.Errorf("%w: read assistant context: %w", ErrContext, err)
		}
		itemPtr = &item
		contextLabel = item.Title
	}

	now := locator.Now
	if now.IsZero() {
		now = time.Now()
	}
	appCtx := ApplicationContext{
		Now:        now,
		Surface:    locator.Surface,
		WorkItemID: trimmedID,
	}

	iterResult, err := s.runtime.IterateWithContext(ctx, requestID, message, appCtx, itemPtr)
	if err != nil {
		if errors.Is(err, ErrRuntime) {
			return Result{}, ErrRuntime
		}
		return Result{}, err
	}

	kind := iterResult.Kind
	if kind == KindMutationCandidate {
		kind = KindMutationIntent
	}

	candidate := iterResult.Candidate
	if iterResult.NoChange {
		candidate = nil
	}

	return Result{
		Kind:          kind,
		Text:          iterResult.Text,
		ContextLabel:  contextLabel,
		ChangeApplied: false,
		Candidate:     candidate,
		Plan:          iterResult.Plan,
		Base:          iterResult.Base,
		NoChange:      iterResult.NoChange,
		ReadRequest:   iterResult.ReadRequest,
		ReadResult:    iterResult.ReadResult,
		Evidence:      iterResult.Evidence,
		Provenance:    iterResult.Provenance,
	}, nil
}

type workItemContext struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Lifecycle   string  `json:"lifecycleState"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	CompletedAt *string `json:"completedAt"`
}

type contextEnvelope struct {
	Surface  string            `json:"surface"`
	WorkItem workItemContext   `json:"selectedWorkItem"`
	Tracking *TrackingSnapshot `json:"tracking,omitempty"`
}
