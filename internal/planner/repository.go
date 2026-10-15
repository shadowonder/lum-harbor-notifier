package planner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/temporal"
	"time"
)

// ErrNotFound is returned when the requested active Planner Item
// does not exist.
var ErrNotFound = errors.New("Planner item not found.")

// Repository persists Planner domain state through the executor supplied at
// construction time. A Repository bound to *sql.DB may be reused for process-
// scoped reads; a Repository bound to *sql.Tx must remain inside that
// transaction's mutation boundary.
type Repository struct {
	queries *dbgen.Queries
}

// NewRepository binds Planner persistence operations to db.
//
// The caller owns the executor lifetime. In particular, services must create a
// new Repository for each transaction rather than reuse a DB-scoped instance.
func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{
		queries: dbgen.New(db),
	}
}

// GetByID loads one active Planner Item.
//
// sqlc handles:
//   - SQL execution
//   - parameter binding
//   - row Scan mapping
func (r *Repository) GetByID(
	ctx context.Context,
	id string,
) (*PlannerItem, error) {

	dbItem, err := r.queries.GetPlannerItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}

		return nil, fmt.Errorf(
			"load planner item %s: %w",
			id,
			err,
		)
	}

	item, err := plannerItemFromDB(dbItem)
	if err != nil {
		return nil, fmt.Errorf(
			"convert planner item %s: %w",
			id,
			err,
		)
	}

	return item, nil
}

func plannerItemFromDB(dbItem dbgen.PlannerItem) (*PlannerItem, error) {

	authority := AuthorityType(dbItem.AuthorityType)
	if !authority.Valid() {
		return nil, fmt.Errorf(
			"invalid authority type %q",
			dbItem.AuthorityType,
		)
	}

	itemType := ItemType(dbItem.ItemType)
	if !itemType.Valid() {
		return nil, fmt.Errorf(
			"invalid planner item type %q",
			dbItem.ItemType,
		)
	}

	status := Status(dbItem.Status)
	if !status.Valid() {
		return nil, fmt.Errorf(
			"invalid planner status %q",
			dbItem.Status,
		)
	}

	contextStatus := ContextStatus(dbItem.ContextStatus)
	if !contextStatus.Valid() {
		return nil, fmt.Errorf(
			"invalid context status %q",
			dbItem.ContextStatus,
		)
	}

	createdAt, err := codec.ParseTime(dbItem.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	updatedAt, err := codec.ParseTime(dbItem.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	scheduledAt, err := codec.ParseOptionalTime(dbItem.ScheduledAt)
	if err != nil {
		return nil, fmt.Errorf("parse scheduled_at: %w", err)
	}

	dueAt, err := codec.ParseOptionalTime(dbItem.DueAt)
	if err != nil {
		return nil, fmt.Errorf("parse due_at: %w", err)
	}

	scheduledDate, err := codec.ParseOptionalLocalDate(dbItem.ScheduledDate)
	if err != nil {
		return nil, fmt.Errorf("parse scheduled_date: %w", err)
	}

	dueDate, err := codec.ParseOptionalLocalDate(dbItem.DueDate)
	if err != nil {
		return nil, fmt.Errorf("parse due_date: %w", err)
	}

	completedAt, err := codec.ParseOptionalTime(dbItem.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("parse completed_at: %w", err)
	}

	missingContext, err := parseMissingContext(
		dbItem.MissingContextJson,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"parse missing_context_json: %w",
			err,
		)
	}

	return &PlannerItem{
		ID:              dbItem.EntityID,
		Authority:       authority,
		WorkItemID:      dbItem.WorkItemID,
		WorkflowID:      dbItem.WorkflowID,
		WorkflowStepID:  dbItem.WorkflowStepID,
		RelatedEntityID: dbItem.RelatedEntityID,
		MonitorPointID:  dbItem.MonitorPointID,
		Type:            itemType,
		Title:           dbItem.Title,
		Details:         dbItem.Details,
		Status:          status,
		ScheduledAt:     scheduledAt,
		DueAt:           dueAt,
		ScheduledDate:   scheduledDate,
		DueDate:         dueDate,
		CompletedAt:     completedAt,
		ContextStatus:   contextStatus,
		MissingContext:  missingContext,
		LastRevisionID:  dbItem.LastRevisionID,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		LocalNote:       dbItem.LocalNote,
	}, nil
}

// plannerItemFromCompleteRow adapts sqlc's UPDATE ... RETURNING row
// into the normal planner_items persistence model so the canonical
// DB-to-domain mapper can be reused.
func plannerItemFromCompleteRow(row dbgen.PlannerItem) (*PlannerItem, error) {
	return plannerItemFromDB(dbgen.PlannerItem{
		EntityID:           row.EntityID,
		AuthorityType:      row.AuthorityType,
		WorkItemID:         row.WorkItemID,
		WorkflowID:         row.WorkflowID,
		WorkflowStepID:     row.WorkflowStepID,
		RelatedEntityID:    row.RelatedEntityID,
		MonitorPointID:     row.MonitorPointID,
		ItemType:           row.ItemType,
		Title:              row.Title,
		Details:            row.Details,
		Status:             row.Status,
		ScheduledAt:        row.ScheduledAt,
		DueAt:              row.DueAt,
		ScheduledDate:      row.ScheduledDate,
		DueDate:            row.DueDate,
		ContextStatus:      row.ContextStatus,
		MissingContextJson: row.MissingContextJson,
		LastRevisionID:     row.LastRevisionID,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		CompletedAt:        row.CompletedAt,
		LocalNote:          row.LocalNote,
	})
}

// parseMissingContext converts the nullable JSON text generated by sqlc
// into the Planner domain's []string representation.
//
// nil / empty value means there are currently no explicit missing-context
// entries stored for the Planner Item.
func parseMissingContext(value *string) ([]string, error) {

	if value == nil || *value == "" {
		return nil, nil
	}

	var missing []string

	if err := json.Unmarshal([]byte(*value), &missing); err != nil {
		return nil, err
	}

	return missing, nil
}

// Create persists a PlannerItem using the database executor bound to
// this Repository.
//
// The Repository does not start or commit transactions and does not
// create Revisions. The Service layer owns those concerns so one business
// operation can atomically modify multiple repositories.
func (r *Repository) Create(ctx context.Context, item *PlannerItem) error {
	params, err := plannerItemToCreateParams(item)
	if err != nil {
		return fmt.Errorf("convert planner item for create: %w", err)
	}

	if err := r.queries.CreatePlannerItem(ctx, params); err != nil {
		return fmt.Errorf("create planner item %s: %w", item.ID, err)
	}

	return nil
}

// plannerItemToCreateParams converts the Planner domain model into
// the persistence parameters generated by sqlc.
//
// sqlc already handles:
//   - SQL text
//   - parameter binding
//   - database execution
//
// This function only converts domain representations into database
// representations:
//
//	time.Time    → RFC3339 string
//	LocalDate    → YYYY-MM-DD string
//	[]string     → JSON text
//	domain types → string
func plannerItemToCreateParams(item *PlannerItem) (dbgen.CreatePlannerItemParams, error) {
	missingContextJSON, err := encodeMissingContext(
		item.MissingContext,
	)
	if err != nil {
		return dbgen.CreatePlannerItemParams{},
			fmt.Errorf("encode missing context: %w", err)
	}

	return dbgen.CreatePlannerItemParams{
		EntityID:           item.ID,
		AuthorityType:      string(item.Authority),
		WorkItemID:         item.WorkItemID,
		WorkflowID:         item.WorkflowID,
		WorkflowStepID:     item.WorkflowStepID,
		RelatedEntityID:    item.RelatedEntityID,
		MonitorPointID:     item.MonitorPointID,
		ItemType:           string(item.Type),
		Title:              item.Title,
		Details:            item.Details,
		Status:             string(item.Status),
		ScheduledAt:        codec.FormatOptionalTime(item.ScheduledAt),
		DueAt:              codec.FormatOptionalTime(item.DueAt),
		ScheduledDate:      codec.FormatOptionalLocalDate(item.ScheduledDate),
		DueDate:            codec.FormatOptionalLocalDate(item.DueDate),
		ContextStatus:      string(item.ContextStatus),
		MissingContextJson: missingContextJSON,
		LastRevisionID:     item.LastRevisionID,
		CreatedAt:          codec.FormatTime(item.CreatedAt),
		UpdatedAt:          codec.FormatTime(item.UpdatedAt),
		CompletedAt:        codec.FormatOptionalTime(item.CompletedAt),
	}, nil
}

// encodeMissingContext converts Lum Harbor's domain representation of
// missing context into the JSON text stored in SQLite.
//
// An empty slice is stored as NULL rather than "[]" because there is
// no meaningful missing-context payload to persist.
func encodeMissingContext(missing []string) (*string, error) {

	if len(missing) == 0 {
		return nil, nil
	}

	data, err := json.Marshal(missing)
	if err != nil {
		return nil, err
	}

	value := string(data)
	return &value, nil
}

// Complete marks one persisted Planner Item as completed.
//
// This method does not decide whether completing the item is allowed.
// Authority and lifecycle rules belong to PlannerService.
func (r *Repository) Complete(
	ctx context.Context,
	id string,
	completedAt time.Time,
	revisionID string,
) (*PlannerItem, error) {

	completedAtValue := codec.FormatTime(completedAt)
	updatedAtValue := completedAtValue

	row, err := r.queries.CompletePlannerItem(
		ctx,
		dbgen.CompletePlannerItemParams{
			EntityID:       id,
			CompletedAt:    &completedAtValue,
			UpdatedAt:      updatedAtValue,
			LastRevisionID: revisionID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("complete planner item %s: %w", id, err)
	}

	item, err := plannerItemFromCompleteRow(row)
	if err != nil {
		return nil, fmt.Errorf("convert completed planner item %s: %w", id, err)
	}

	return item, nil
}

// UpdateExternalNativeStateParams is the full native-state overwrite one
// deterministic external-sync refresh applies. scheduled_at/scheduled_date
// are never included: external sync never sets Lum Harbor-only fields.
type UpdateExternalNativeStateParams struct {
	EntityID       string
	Title          string
	Details        *string
	DueAt          *time.Time
	DueDate        *temporal.LocalDate
	Status         Status
	CompletedAt    *time.Time
	UpdatedAt      time.Time
	LastRevisionID string
}

// UpdateExternalNativeState refreshes an EXTERNAL Planner Item's native
// fields. It matches only rows with authority_type = 'EXTERNAL'
// (UpdateExternalPlannerItemNativeState's own WHERE clause), so it returns
// ErrNotFound both when entityID does not exist and when it names a LOCAL
// item; PlannerService distinguishes those cases with an authoritative read
// before calling this, so reaching either branch here is a genuine race, not
// the normal authority check.
func (r *Repository) UpdateExternalNativeState(ctx context.Context, params UpdateExternalNativeStateParams) (*PlannerItem, error) {
	row, err := r.queries.UpdateExternalPlannerItemNativeState(ctx, dbgen.UpdateExternalPlannerItemNativeStateParams{
		EntityID:       params.EntityID,
		Title:          params.Title,
		Details:        params.Details,
		DueAt:          codec.FormatOptionalTime(params.DueAt),
		DueDate:        codec.FormatOptionalLocalDate(params.DueDate),
		Status:         string(params.Status),
		CompletedAt:    codec.FormatOptionalTime(params.CompletedAt),
		UpdatedAt:      codec.FormatTime(params.UpdatedAt),
		LastRevisionID: params.LastRevisionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, params.EntityID)
		}
		return nil, fmt.Errorf("update external planner item native state %s: %w", params.EntityID, err)
	}
	item, err := plannerItemFromDB(row)
	if err != nil {
		return nil, fmt.Errorf("convert updated external planner item %s: %w", params.EntityID, err)
	}
	return item, nil
}

// UpdateLocalNote replaces only the Lum Harbor-owned local_note of one
// Planner Item. It never touches an external-authoritative column; authority
// and stale checks belong to PlannerService.
func (r *Repository) UpdateLocalNote(ctx context.Context, id string, localNote *string, updatedAt time.Time, revisionID string) (*PlannerItem, error) {
	row, err := r.queries.UpdatePlannerItemLocalNote(ctx, dbgen.UpdatePlannerItemLocalNoteParams{
		EntityID:       id,
		LocalNote:      localNote,
		UpdatedAt:      codec.FormatTime(updatedAt),
		LastRevisionID: revisionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("update planner item local note %s: %w", id, err)
	}
	item, err := plannerItemFromDB(row)
	if err != nil {
		return nil, fmt.Errorf("convert planner item %s: %w", id, err)
	}
	return item, nil
}

// ListCurrentOpen returns active OPEN Planner Items that belong to the
// user's current action queue.
func (r *Repository) ListCurrentOpen(ctx context.Context, today temporal.LocalDate, tomorrowStart time.Time) ([]PlannerItem, error) {

	if !today.Valid() {
		return nil, fmt.Errorf("invalid current local date %q", today)
	}

	todayValue := string(today)
	tomorrowStartValue := codec.FormatTime(tomorrowStart)

	rows, err := r.queries.ListCurrentOpenPlannerItems(
		ctx,
		dbgen.ListCurrentOpenPlannerItemsParams{
			TomorrowStart: &tomorrowStartValue,
			TodayDate:     &todayValue,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list current open planner items: %w", err)
	}

	items := make([]PlannerItem, 0, len(rows))

	for _, row := range rows {
		item, err := plannerItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf(
				"convert current open planner item %s: %w",
				row.EntityID,
				err,
			)
		}

		items = append(items, *item)
	}

	return items, nil
}

// ListCompletedByRange returns Planner Items completed within the
// requested half-open instant range [start, end).
//
// The range is based on completed_at, not scheduled or due time.
func (r *Repository) ListCompletedByRange(ctx context.Context, start time.Time, end time.Time) ([]PlannerItem, error) {

	if !end.After(start) {
		return nil, fmt.Errorf("completed planner range end must be after start")
	}

	startValue := codec.FormatTime(start)
	endValue := codec.FormatTime(end)

	rows, err := r.queries.ListCompletedPlannerItemsByRange(
		ctx,
		dbgen.ListCompletedPlannerItemsByRangeParams{
			RangeStart: &startValue,
			RangeEnd:   &endValue,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list completed planner items by range: %w", err)
	}

	items := make([]PlannerItem, 0, len(rows))

	for _, row := range rows {
		item, err := plannerItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf(
				"convert completed planner item %s: %w",
				row.EntityID,
				err,
			)
		}

		items = append(items, *item)
	}

	return items, nil
}

func (r *Repository) ListByRange(
	ctx context.Context,
	startAt time.Time,
	endAt time.Time,
	startDate temporal.LocalDate,
	endDate temporal.LocalDate,
) ([]PlannerItem, error) {

	if !endAt.After(startAt) {
		return nil, fmt.Errorf("planner range end must be after start")
	}
	if !startDate.Valid() {
		return nil, fmt.Errorf("invalid planner range start date %q", startDate)
	}
	if !endDate.Valid() {
		return nil, fmt.Errorf("invalid planner range end date %q", endDate)
	}
	if string(endDate) <= string(startDate) {
		return nil, fmt.Errorf("planner date range end must be after start")
	}

	startAtValue := codec.FormatTime(startAt)
	endAtValue := codec.FormatTime(endAt)

	startDateValue := string(startDate)
	endDateValue := string(endDate)

	rows, err := r.queries.ListPlannerItemsByRange(
		ctx,
		dbgen.ListPlannerItemsByRangeParams{
			RangeStartAt:   &startAtValue,
			RangeEndAt:     &endAtValue,
			RangeStartDate: &startDateValue,
			RangeEndDate:   &endDateValue,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list planner items by range: %w", err)
	}

	items := make([]PlannerItem, 0, len(rows))

	for _, row := range rows {
		item, err := plannerItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf(
				"convert planner item %s: %w",
				row.EntityID,
				err,
			)
		}
		items = append(items, *item)
	}

	return items, nil
}

// SearchByTitle returns up to limit current Planner Items whose title
// contains query as a literal, case-insensitive substring, ordered by title
// then entity ID for a deterministic result (M15 Slice 1 readcap
// entity.search capability).
func (r *Repository) SearchByTitle(ctx context.Context, query string, limit int32) ([]PlannerItem, error) {
	rows, err := r.queries.SearchPlannerItemsByTitle(ctx, dbgen.SearchPlannerItemsByTitleParams{
		Query: query, RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search planner items by title: %w", err)
	}
	items := make([]PlannerItem, 0, len(rows))
	for _, row := range rows {
		item, err := plannerItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf("convert planner item %s: %w", row.EntityID, err)
		}
		items = append(items, *item)
	}
	return items, nil
}
