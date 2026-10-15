-- name: ListCalendarEventsByRange :many
SELECT
    c.entity_id,
    c.authority_type,
    c.title,
    c.details,
    c.location,
    c.start_at,
    c.end_at,
    c.start_date,
    c.end_date,
    c.is_all_day,
    c.timezone,
    c.last_revision_id,
    c.created_at,
    c.updated_at
FROM calendar_events c
INNER JOIN entities e ON e.id = c.entity_id
WHERE
    e.archived_at IS NULL
    AND ((
        c.is_all_day = 0 AND c.start_at < sqlc.arg(range_end_at) AND c.end_at > sqlc.arg(range_start_at)
    ) OR (
        c.is_all_day = 1 AND c.start_date < sqlc.arg(range_end_date) AND c.end_date > sqlc.arg(range_start_date)
    ))
ORDER BY
    c.is_all_day DESC,
    c.start_date ASC,
    c.start_at ASC,
    c.created_at ASC;