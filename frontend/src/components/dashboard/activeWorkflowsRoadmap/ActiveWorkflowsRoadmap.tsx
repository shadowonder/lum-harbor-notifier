import { Box, Tooltip, Typography, ButtonBase } from '@mui/material';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  buildRoadmapHoverSummary,
  computeAdaptivePixelsPerDay,
  computeDayOffset,
  computeRoadmapWindow,
  computeTimelineTickGroups,
  computeTimelineWidth,
  daysBetweenLocalDates,
  describeTargetDate,
  formatDateDescriptionLabel,
  formatRoadmapDateLabel,
  timelineDateKeys,
  ROADMAP_IDENTITY_COLUMN_WIDTH,
  ROADMAP_PANEL_MAX_HEIGHT,
  ROADMAP_PANEL_MIN_HEIGHT,
  ROADMAP_ROW_HEIGHT,
  type ActiveWorkflowRoadmapRow,
} from './roadmapLayout.ts';

const HEADER_HEIGHT = 40;
const MARKER_SIZE = 10;
const START_MARKER_SIZE = 10;
const SPAN_LINE_HEIGHT = 3;

function HoverSummaryContent({ row, today }: { row: ActiveWorkflowRoadmapRow; today: string }) {
  const summary = buildRoadmapHoverSummary(row, today);
  return (
    <Box sx={{ py: 0.25 }}>
      <Typography variant="body2" sx={{ fontWeight: 650 }}>
        {summary.title}
      </Typography>
      {summary.description && (
        <Typography variant="caption" sx={{ display: 'block', mt: 0.25, opacity: 0.85 }}>
          {summary.description}
        </Typography>
      )}
      {(summary.startLabel || summary.currentTargetLabel) && (
        <Box sx={{ mt: 0.75, display: 'grid', gridTemplateColumns: 'auto auto', columnGap: 1 }}>
          {summary.startLabel && (
            <>
              <Typography variant="caption" sx={{ opacity: 0.7 }}>
                Start
              </Typography>
              <Typography variant="caption">{summary.startLabel}</Typography>
            </>
          )}
          {summary.currentTargetLabel && (
            <>
              <Typography variant="caption" sx={{ opacity: 0.7 }}>
                Target
              </Typography>
              <Typography variant="caption">{summary.currentTargetLabel}</Typography>
            </>
          )}
          {summary.previousTargetLabel && (
            <>
              <Typography variant="caption" sx={{ opacity: 0.7 }}>
                Previous
              </Typography>
              <Typography variant="caption">{summary.previousTargetLabel}</Typography>
            </>
          )}
          {summary.scheduleRevisionLabel && (
            <>
              <Typography variant="caption" sx={{ opacity: 0.7 }}>
                Schedule
              </Typography>
              <Typography variant="caption">{summary.scheduleRevisionLabel}</Typography>
            </>
          )}
        </Box>
      )}
      {summary.dateStateLabel && (
        <Typography variant="caption" sx={{ display: 'block', mt: 0.75, fontWeight: 650 }}>
          {summary.dateStateLabel}
        </Typography>
      )}
    </Box>
  );
}

/**
 * Dashboard Active Workflows Roadmap — read-only, date-first projection.
 *
 * Single-ScrollViewport architecture (mirrors the WorkItem Dependency
 * Explorer's proven pattern): exactly one element below owns both
 * `overflowX`/`overflowY`; the identity column and the date header are
 * `position: sticky` descendants of that same element, never separate
 * nested scroll boxes. This keeps the vertical scrollbar at the panel's
 * true right edge and avoids the two previously-debugged bug classes
 * (a nested scroll box's gutter bleeding into the outer overflow
 * calculation, and a scrollbar rendering at a narrow inner content edge).
 *
 * Experiment 1.1: the timeline's pixels-per-day is measured against this
 * same viewport (see `computeAdaptivePixelsPerDay`) so a normal date span
 * fills the available width instead of leaving a large dead whitespace
 * gutter — never a `transform: scale()`, never a font-size change.
 */
export default function ActiveWorkflowsRoadmap({
  rows,
  today,
  onOpenWorkItem,
}: {
  rows: ActiveWorkflowRoadmapRow[];
  today: string;
  onOpenWorkItem: (id: string, title: string) => void;
}) {
  // Measured purely to pick a pixels-per-day value within [MIN, MAX] — never
  // to apply a scale transform. Logical geometry (row height, font size,
  // marker size) never changes because of this measurement.
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const [viewportWidth, setViewportWidth] = useState<number | null>(null);
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return undefined;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width;
      if (width) setViewportWidth(width);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const window_ = useMemo(() => {
    const allDateKeys = rows.flatMap((row) => timelineDateKeys(row.timeline));
    return computeRoadmapWindow(today, allDateKeys);
  }, [rows, today]);

  const tickGroups = useMemo(() => computeTimelineTickGroups(window_), [window_]);
  const visibleSpanDays = daysBetweenLocalDates(window_.startDate, window_.endDate);
  // Before the first real measurement, fall back to a sensible default
  // scale rather than guessing 0 (which would otherwise floor at MIN).
  const availableTimelineWidth = (viewportWidth ?? 0) - ROADMAP_IDENTITY_COLUMN_WIDTH;
  const pixelsPerDay = computeAdaptivePixelsPerDay(visibleSpanDays, availableTimelineWidth);
  const timelineWidth = computeTimelineWidth(window_, pixelsPerDay);
  const totalWidth = ROADMAP_IDENTITY_COLUMN_WIDTH + timelineWidth;
  const rowsHeight = rows.length * ROADMAP_ROW_HEIGHT;
  const offsetPx = (dateKey: string) => computeDayOffset(dateKey, window_.startDate) * pixelsPerDay;
  const todayOffsetPx = offsetPx(today);

  if (rows.length === 0) {
    return (
      <Box sx={{ px: 2.25, py: 3 }}>
        <Typography variant="body2" color="text.secondary">
          No active workflows with a target date.
        </Typography>
      </Box>
    );
  }

  return (
    <Box
      ref={viewportRef}
      sx={{
        width: '100%',
        overflowX: 'auto',
        overflowY: 'auto',
        minHeight: ROADMAP_PANEL_MIN_HEIGHT,
        maxHeight: `min(${ROADMAP_PANEL_MAX_HEIGHT}px, 45vh)`,
      }}
    >
      <Box sx={{ width: totalWidth, minWidth: '100%' }}>
        {/* Date header: sticky to the top of the single ScrollViewport. */}
        <Box
          sx={{
            display: 'flex',
            position: 'sticky',
            top: 0,
            zIndex: 3,
            bgcolor: 'background.paper',
            borderBottom: 1,
            borderColor: 'divider',
          }}
        >
          <Box
            sx={{
              width: ROADMAP_IDENTITY_COLUMN_WIDTH,
              flexShrink: 0,
              position: 'sticky',
              left: 0,
              zIndex: 4,
              bgcolor: 'background.paper',
              display: 'flex',
              alignItems: 'flex-end',
              px: 2.25,
              pb: 0.75,
            }}
          >
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontWeight: 700, letterSpacing: '0.08em' }}
            >
              WORK
            </Typography>
          </Box>
          <Box
            sx={{
              width: timelineWidth,
              flexShrink: 0,
              position: 'relative',
              height: HEADER_HEIGHT,
            }}
          >
            {tickGroups.map((group) => {
              const groupStartPx = offsetPx(group.ticks[0]!.dateKey);
              return (
                <Box
                  key={`${group.label}-${group.ticks[0]!.dateKey}`}
                  sx={{ position: 'absolute', left: groupStartPx, top: 0 }}
                >
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ fontWeight: 700, letterSpacing: '0.08em', display: 'block' }}
                  >
                    {group.label}
                  </Typography>
                  <Box sx={{ display: 'flex', gap: '18px', mt: 0.25 }}>
                    {group.ticks.map((tick) => (
                      <Typography key={tick.dateKey} variant="caption" color="text.disabled">
                        {tick.dayLabel}
                      </Typography>
                    ))}
                  </Box>
                </Box>
              );
            })}
            <Typography
              variant="caption"
              sx={{
                position: 'absolute',
                left: todayOffsetPx,
                top: 0,
                transform: 'translateX(-50%)',
                fontWeight: 700,
                letterSpacing: '0.08em',
                color: 'text.primary',
              }}
            >
              TODAY
            </Typography>
          </Box>
        </Box>

        {/* Rows: fixed row height; each row's identity cell stays sticky-left. */}
        <Box sx={{ position: 'relative', width: totalWidth, height: rowsHeight }}>
          {/* Today: rendered above the row bars (interleaved, never covered by them). */}
          <Box
            aria-hidden="true"
            sx={{
              position: 'absolute',
              left: ROADMAP_IDENTITY_COLUMN_WIDTH + todayOffsetPx,
              top: 0,
              width: '1px',
              height: rowsHeight,
              bgcolor: 'text.primary',
              opacity: 0.35,
              zIndex: 1,
            }}
          />
          {rows.map((row, index) => {
            const timeline = row.timeline;
            const description =
              timeline.kind === 'unscheduled'
                ? null
                : describeTargetDate(
                    timeline.kind === 'milestone'
                      ? timeline.targetDateKey
                      : timeline.currentTargetDateKey,
                    today,
                  );
            const isOverdue = description?.state === 'overdue';
            const barColor = isOverdue ? 'warning.main' : 'primary.main';
            const activate = () => onOpenWorkItem(row.workItemId, row.title);

            const identitySummary =
              timeline.kind === 'unscheduled'
                ? 'Unscheduled'
                : timeline.kind === 'milestone'
                  ? `Target ${formatRoadmapDateLabel(timeline.targetDateKey)} · ${formatDateDescriptionLabel(description!)}`
                  : `Started ${formatRoadmapDateLabel(timeline.startDateKey)} · Target ${formatRoadmapDateLabel(timeline.currentTargetDateKey)}`;

            return (
              <Tooltip
                key={row.workItemId}
                title={<HoverSummaryContent row={row} today={today} />}
                placement="top"
                enterDelay={350}
              >
                <Box
                  sx={{
                    position: 'absolute',
                    top: index * ROADMAP_ROW_HEIGHT,
                    left: 0,
                    display: 'flex',
                    width: totalWidth,
                    height: ROADMAP_ROW_HEIGHT,
                    bgcolor: index % 2 === 1 ? 'surface.subtle' : 'transparent',
                    borderTop: 1,
                    borderColor: 'divider',
                  }}
                >
                  <Box
                    sx={{
                      width: ROADMAP_IDENTITY_COLUMN_WIDTH,
                      flexShrink: 0,
                      position: 'sticky',
                      left: 0,
                      zIndex: 2,
                      bgcolor: index % 2 === 1 ? 'surface.subtle' : 'background.paper',
                      display: 'flex',
                      alignItems: 'center',
                    }}
                  >
                    <ButtonBase
                      onClick={activate}
                      sx={{
                        width: '100%',
                        height: '100%',
                        px: 2.25,
                        justifyContent: 'flex-start',
                        textAlign: 'left',
                        '&:hover': { bgcolor: 'action.hover' },
                        '&:focus-visible': {
                          outline: '2px solid',
                          outlineColor: 'primary.main',
                          outlineOffset: -2,
                        },
                      }}
                    >
                      <Box sx={{ minWidth: 0 }}>
                        <Typography sx={{ fontWeight: 600 }} noWrap>
                          {row.title}
                        </Typography>
                        <Typography
                          variant="caption"
                          color={isOverdue ? 'warning.main' : 'text.secondary'}
                          noWrap
                          sx={{ display: 'block' }}
                        >
                          {identitySummary}
                        </Typography>
                      </Box>
                    </ButtonBase>
                  </Box>
                  <Box
                    onClick={activate}
                    sx={{
                      width: timelineWidth,
                      flexShrink: 0,
                      position: 'relative',
                      cursor: 'pointer',
                      '&:hover': { bgcolor: 'action.hover' },
                    }}
                  >
                    {timeline.kind === 'span' && (
                      <>
                        {/* One restrained span: a rounded line from a hollow start circle to the target diamond. */}
                        <Box
                          data-testid="span-line"
                          sx={{
                            position: 'absolute',
                            left: offsetPx(timeline.startDateKey),
                            top: '50%',
                            transform: 'translateY(-50%)',
                            width:
                              offsetPx(
                                timeline.previousTargetDateKey ?? timeline.currentTargetDateKey,
                              ) - offsetPx(timeline.startDateKey),
                            height: SPAN_LINE_HEIGHT,
                            borderRadius: SPAN_LINE_HEIGHT / 2,
                            bgcolor: barColor,
                            opacity: 0.5,
                          }}
                        />
                        {/* Start: hollow circle (distinct from the filled target diamond). */}
                        <Box
                          data-testid="span-start-marker"
                          sx={{
                            position: 'absolute',
                            left: offsetPx(timeline.startDateKey) - START_MARKER_SIZE / 2,
                            top: '50%',
                            width: START_MARKER_SIZE,
                            height: START_MARKER_SIZE,
                            transform: 'translateY(-50%)',
                            borderRadius: '50%',
                            border: '2px solid',
                            borderColor: barColor,
                            bgcolor: 'background.paper',
                            boxSizing: 'border-box',
                          }}
                        />
                        {timeline.previousTargetDateKey && (
                          <>
                            {/* Revision extension: previous target -> current target, restrained/dashed. */}
                            <Box
                              sx={{
                                position: 'absolute',
                                left: offsetPx(timeline.previousTargetDateKey),
                                top: '50%',
                                width:
                                  offsetPx(timeline.currentTargetDateKey) -
                                  offsetPx(timeline.previousTargetDateKey),
                                borderTop: '2px dashed',
                                borderColor: barColor,
                                opacity: 0.55,
                              }}
                            />
                            {/* Previous target: hollow diamond. */}
                            <Box
                              sx={{
                                position: 'absolute',
                                left: offsetPx(timeline.previousTargetDateKey) - MARKER_SIZE / 2,
                                top: '50%',
                                width: MARKER_SIZE,
                                height: MARKER_SIZE,
                                transform: 'translateY(-50%) rotate(45deg)',
                                border: '2px solid',
                                borderColor: barColor,
                                bgcolor: 'background.paper',
                              }}
                            />
                          </>
                        )}
                        {/* Current target: filled diamond. */}
                        <Box
                          data-testid="span-target-marker"
                          sx={{
                            position: 'absolute',
                            left: offsetPx(timeline.currentTargetDateKey) - MARKER_SIZE / 2,
                            top: '50%',
                            width: MARKER_SIZE,
                            height: MARKER_SIZE,
                            transform: 'translateY(-50%) rotate(45deg)',
                            bgcolor: barColor,
                          }}
                        />
                      </>
                    )}
                    {timeline.kind === 'milestone' && (
                      <Box
                        sx={{
                          position: 'absolute',
                          left: offsetPx(timeline.targetDateKey) - MARKER_SIZE / 2,
                          top: '50%',
                          width: MARKER_SIZE,
                          height: MARKER_SIZE,
                          transform: 'translateY(-50%) rotate(45deg)',
                          bgcolor: barColor,
                        }}
                      />
                    )}
                    {timeline.kind === 'unscheduled' && (
                      <Typography
                        variant="caption"
                        color="text.disabled"
                        sx={{
                          position: 'absolute',
                          left: 8,
                          top: '50%',
                          transform: 'translateY(-50%)',
                        }}
                      >
                        No target date
                      </Typography>
                    )}
                  </Box>
                </Box>
              </Tooltip>
            );
          })}
        </Box>
      </Box>
    </Box>
  );
}
