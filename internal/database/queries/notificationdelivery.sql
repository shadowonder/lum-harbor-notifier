-- name: RecordNotificationExternalDelivery :exec
INSERT INTO notification_external_deliveries (notification_id, delivered_at)
VALUES (?, ?);

-- name: GetNotificationExternalDelivery :one
SELECT notification_id, delivered_at
FROM notification_external_deliveries
WHERE notification_id = ?;

-- name: ListSentNotificationsAwaitingExternalDelivery :many
SELECT
    n.entity_id AS notification_id,
    n.title,
    n.message,
    a.severity,
    n.planner_item_id,
    n.work_item_id
FROM notifications n
INNER JOIN attention_issues a ON a.entity_id = n.attention_issue_id
LEFT JOIN notification_external_deliveries d ON d.notification_id = n.entity_id
WHERE n.state = 'SENT' AND d.notification_id IS NULL
ORDER BY n.created_at ASC, n.entity_id ASC;
