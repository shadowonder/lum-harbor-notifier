-- name: CreateAttentionIssue :exec
INSERT INTO attention_issues (
    entity_id, work_item_id, subject_tracked_item_id, monitor_point_id, planner_item_id, issue_type, severity, state,
    reason, reason_json, recommended_action, opened_at, resolved_at, snoozed_until, dedupe_key,
    last_revision_id, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAttentionIssueByID :one
SELECT
    a.entity_id, a.work_item_id, a.subject_tracked_item_id, a.monitor_point_id, a.planner_item_id, a.issue_type, a.severity, a.state,
    a.reason, a.reason_json, a.recommended_action, a.opened_at, a.resolved_at, a.snoozed_until, a.dedupe_key,
    a.last_revision_id, a.updated_at
FROM attention_issues a
INNER JOIN entities e ON e.id = a.entity_id
WHERE a.entity_id = ? AND e.archived_at IS NULL;

-- name: GetOpenAttentionIssueByDedupeKey :one
SELECT
    a.entity_id, a.work_item_id, a.subject_tracked_item_id, a.monitor_point_id, a.planner_item_id, a.issue_type, a.severity, a.state,
    a.reason, a.reason_json, a.recommended_action, a.opened_at, a.resolved_at, a.snoozed_until, a.dedupe_key,
    a.last_revision_id, a.updated_at
FROM attention_issues a
WHERE a.dedupe_key = ? AND a.state = 'OPEN';

-- name: ListOpenAttentionIssues :many
SELECT
    a.entity_id, a.work_item_id, a.subject_tracked_item_id, a.monitor_point_id, a.planner_item_id, a.issue_type, a.severity, a.state,
    a.reason, a.reason_json, a.recommended_action, a.opened_at, a.resolved_at, a.snoozed_until, a.dedupe_key,
    a.last_revision_id, a.updated_at
FROM attention_issues a
INNER JOIN entities e ON e.id = a.entity_id
WHERE a.state = 'OPEN' AND e.archived_at IS NULL
ORDER BY a.opened_at ASC, a.entity_id ASC;

-- name: ListAttentionIssues :many
SELECT
    a.entity_id, a.work_item_id, a.subject_tracked_item_id, a.monitor_point_id, a.planner_item_id, a.issue_type, a.severity, a.state,
    a.reason, a.reason_json, a.recommended_action, a.opened_at, a.resolved_at, a.snoozed_until, a.dedupe_key,
    a.last_revision_id, a.updated_at
FROM attention_issues a
INNER JOIN entities e ON e.id = a.entity_id
WHERE e.archived_at IS NULL
ORDER BY a.opened_at DESC, a.entity_id DESC;

-- name: ResolveAttentionIssue :exec
UPDATE attention_issues
SET state = 'RESOLVED',
    resolved_at = sqlc.arg(resolved_at),
    snoozed_until = NULL,
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND state = 'OPEN';

-- name: ResolveAttentionIssuesByPlannerItem :exec
UPDATE attention_issues
SET state = 'RESOLVED',
    resolved_at = sqlc.arg(resolved_at),
    snoozed_until = NULL,
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE planner_item_id = sqlc.arg(planner_item_id) AND state = 'OPEN';

-- name: ListOpenAttentionIssuesByPlannerItem :many
SELECT
    a.entity_id, a.work_item_id, a.subject_tracked_item_id, a.monitor_point_id, a.planner_item_id, a.issue_type, a.severity, a.state,
    a.reason, a.reason_json, a.recommended_action, a.opened_at, a.resolved_at, a.snoozed_until, a.dedupe_key,
    a.last_revision_id, a.updated_at
FROM attention_issues a
WHERE a.planner_item_id = ? AND a.state = 'OPEN';

-- name: SnoozeAttentionIssue :exec
UPDATE attention_issues
SET snoozed_until = sqlc.arg(snoozed_until),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND state = 'OPEN';

-- name: CreateNotification :exec
INSERT INTO notifications (
    entity_id, attention_issue_id, work_item_id, subject_tracked_item_id, planner_item_id, title, message,
    reason_snapshot_json, state, channel, sent_at, presented_at, read_at, dismissed_at, dedupe_key,
    last_revision_id, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetNotificationByID :one
SELECT
    n.entity_id, n.attention_issue_id, n.work_item_id, n.subject_tracked_item_id, n.planner_item_id, n.title, n.message,
    n.reason_snapshot_json, n.state, n.channel, n.sent_at, n.presented_at, n.read_at, n.dismissed_at, n.dedupe_key,
    n.last_revision_id, n.created_at, n.updated_at
FROM notifications n
INNER JOIN entities e ON e.id = n.entity_id
WHERE n.entity_id = ? AND e.archived_at IS NULL;

-- name: ListNotifications :many
SELECT
    n.entity_id, n.attention_issue_id, n.work_item_id, n.subject_tracked_item_id, n.planner_item_id, n.title, n.message,
    n.reason_snapshot_json, n.state, n.channel, n.sent_at, n.presented_at, n.read_at, n.dismissed_at, n.dedupe_key,
    n.last_revision_id, n.created_at, n.updated_at
FROM notifications n
INNER JOIN entities e ON e.id = n.entity_id
WHERE e.archived_at IS NULL
ORDER BY n.created_at DESC, n.entity_id DESC;

-- name: MarkNotificationRead :exec
UPDATE notifications
SET read_at = sqlc.arg(read_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND read_at IS NULL AND state = 'SENT';

-- name: ListNotificationsByState :many
SELECT
    n.entity_id, n.attention_issue_id, n.work_item_id, n.subject_tracked_item_id, n.planner_item_id, n.title, n.message,
    n.reason_snapshot_json, n.state, n.channel, n.sent_at, n.presented_at, n.read_at, n.dismissed_at, n.dedupe_key,
    n.last_revision_id, n.created_at, n.updated_at
FROM notifications n
INNER JOIN entities e ON e.id = n.entity_id
WHERE n.state = ? AND e.archived_at IS NULL
ORDER BY n.created_at ASC, n.entity_id ASC;

-- name: MarkNotificationSent :execrows
UPDATE notifications
SET state = 'SENT',
    sent_at = sqlc.arg(sent_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND state = 'PENDING';

-- name: MarkNotificationPresented :execrows
UPDATE notifications
SET presented_at = sqlc.arg(presented_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND presented_at IS NULL;

-- name: MarkNotificationCancelled :execrows
UPDATE notifications
SET state = 'CANCELLED',
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND state = 'PENDING';

-- name: UpdateAttentionIssueReason :exec
UPDATE attention_issues
SET reason = sqlc.arg(reason),
    reason_json = sqlc.arg(reason_json),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id) AND state = 'OPEN';

