import { useCallback, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type WheelEvent } from 'react';
import { Box, ButtonBase, Divider, Stack, Tooltip, Typography } from '@mui/material';
import type { Attention } from '../../types/workItem';
import {
  createRoadmapDateRange,
  createRoadmapTicks,
  dateToX,
  differenceInRoadmapDays,
  formatRoadmapDate,
  layoutRoadmapBar,
} from '../../presentation/workItems/roadmapDateScale';
import type { RoadmapItemViewModel, RoadmapRange } from '../../presentation/workItems/roadmapViewModels';

const attentionLabels: Record<Attention, string> = {
  high: 'High attention', attention: 'Needs attention', normal: 'On track', low: 'Low attention',
};

const attentionColor: Record<Attention, string> = {
  high: 'error.main', attention: 'warning.main', normal: 'text.secondary', low: 'text.secondary',
};

function RoadmapDetailTooltip({ item }: { item: RoadmapItemViewModel }) {
  const lateDays = item.forecastAt ? differenceInRoadmapDays(item.targetAt, item.forecastAt) : 0;
  return <Box sx={{ width: 245, py: 0.35 }}>
    <Typography variant="body2" sx={{ color: 'inherit', fontWeight: 750 }}>{item.title}</Typography>
    <Typography variant="caption" sx={{ display: 'block', mt: 0.15, color: 'inherit', opacity: 0.75 }}>
      {formatRoadmapDate(item.startAt)} → {formatRoadmapDate(item.targetAt)}
    </Typography>
    <Divider sx={{ my: 0.8, borderColor: 'currentColor', opacity: 0.18 }} />
    <Stack spacing={0.45}>
      <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Progress</Box>{item.progress.label} · {Math.round(item.progress.ratio * 100)}%</Typography>
      <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Attention</Box>{attentionLabels[item.attention]}</Typography>
      {item.latestProgress && <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Latest</Box>{item.latestProgress.date} · {item.latestProgress.label}</Typography>}
      <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Target</Box>{formatRoadmapDate(item.targetAt)}</Typography>
      {lateDays > 0 && item.forecastAt && <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Forecast</Box>{formatRoadmapDate(item.forecastAt)} · +{lateDays} days</Typography>}
      {item.nextAction && <Typography variant="caption" sx={{ color: 'inherit' }}><Box component="span" sx={{ display: 'inline-block', width: 83, opacity: 0.68 }}>Next</Box>{item.nextAction}</Typography>}
    </Stack>
  </Box>;
}

function RangeControls({ value, onChange, onToday }: { value: RoadmapRange; onChange: (range: RoadmapRange) => void; onToday: () => void }) {
  const ranges: RoadmapRange[] = ['2W', '4W', '3M'];
  return <Stack direction="row" spacing={0.65} sx={{ alignItems: 'center' }}>
    <Stack direction="row" sx={{ p: 0.25, borderRadius: 1, bgcolor: 'surface.secondary' }}>
      {ranges.map((range) => <ButtonBase key={range} aria-pressed={value === range} onClick={() => onChange(range)} sx={{ minWidth: 34, px: 0.65, py: 0.35, borderRadius: 0.75, color: value === range ? 'text.primary' : 'text.secondary', bgcolor: value === range ? 'background.paper' : 'transparent', fontSize: 10, fontWeight: 700, boxShadow: value === range ? 1 : 0, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 1 } }}>{range}</ButtonBase>)}
    </Stack>
    <ButtonBase onClick={onToday} sx={{ px: 0.75, py: 0.45, borderRadius: 0.75, color: 'primary.main', fontSize: 10.5, fontWeight: 700, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 1 } }}>Today</ButtonBase>
  </Stack>;
}

export default function DeliveryHorizon({ items, asOf, onOpen }: {
  items: RoadmapItemViewModel[];
  asOf: string;
  onOpen: (item: RoadmapItemViewModel) => void;
}) {
  const [zoom, setZoom] = useState<RoadmapRange>('4W');
  const [panning, setPanning] = useState(false);
  const scrollerRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<{ pointerId: number; startX: number; scrollLeft: number; moved: boolean } | null>(null);
  const suppressClickRef = useRef(false);
  const range = useMemo(() => createRoadmapDateRange(asOf, zoom), [asOf, zoom]);
  const ticks = useMemo(() => createRoadmapTicks(range, zoom), [range, zoom]);
  const todayX = dateToX(range.today, range);

  const scrollToToday = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const labelWidth = scroller.querySelector<HTMLElement>('[data-roadmap-label]')?.getBoundingClientRect().width ?? 220;
    const timelineViewport = Math.max(1, scroller.clientWidth - labelWidth);
    scroller.scrollTo({ left: Math.max(0, todayX - timelineViewport * 0.3), behavior });
  }, [todayX]);

  useLayoutEffect(() => {
    const frame = requestAnimationFrame(() => scrollToToday('auto'));
    return () => cancelAnimationFrame(frame);
  }, [scrollToToday]);

  const onWheel = (event: WheelEvent<HTMLDivElement>) => {
    if (!event.shiftKey || !event.deltaY || !scrollerRef.current) return;
    event.preventDefault();
    scrollerRef.current.scrollLeft += event.deltaY;
  };
  const startPan = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest('[data-roadmap-interactive]')) return;
    panRef.current = { pointerId: event.pointerId, startX: event.clientX, scrollLeft: scrollerRef.current?.scrollLeft ?? 0, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
    setPanning(true);
  };
  const movePan = (event: PointerEvent<HTMLDivElement>) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== event.pointerId || !scrollerRef.current) return;
    const distance = event.clientX - pan.startX;
    if (Math.abs(distance) > 4) pan.moved = true;
    scrollerRef.current.scrollLeft = pan.scrollLeft - distance;
  };
  const finishPan = (event: PointerEvent<HTMLDivElement>) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== event.pointerId) return;
    suppressClickRef.current = pan.moved;
    panRef.current = null;
    setPanning(false);
  };
  const openRow = (item: RoadmapItemViewModel) => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
    onOpen(item);
  };
  const onRowKeyDown = (event: KeyboardEvent<HTMLElement>, item: RoadmapItemViewModel) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    onOpen(item);
  };

  return <Box sx={{ minWidth: 0 }}>
    <Stack direction="row" spacing={2} useFlexGap sx={{ px: 2.25, pt: 1.8, pb: 1.3, alignItems: 'flex-start', justifyContent: 'space-between', flexWrap: 'wrap' }}>
      <Box>
        <Typography component="h2" variant="h2">Delivery Horizon</Typography>
        <Typography variant="caption" color="text.secondary">All active work · sorted by attention and target proximity</Typography>
      </Box>
      <RangeControls value={zoom} onChange={setZoom} onToday={() => scrollToToday()} />
    </Stack>

    <Box
      ref={scrollerRef}
      onWheel={onWheel}
      sx={{
        '--roadmap-label-width': 'clamp(190px, 22cqw, 230px)',
        maxWidth: '100%', maxHeight: 430, overflow: 'auto', overscrollBehavior: 'contain', scrollbarWidth: 'thin',
        '&::-webkit-scrollbar': { width: 8, height: 8 },
        '&::-webkit-scrollbar-thumb': { bgcolor: 'divider', borderRadius: 8, border: '2px solid transparent', backgroundClip: 'padding-box' },
      }}
    >
      <Box sx={{ width: `calc(var(--roadmap-label-width) + ${range.timelineWidth}px)`, minWidth: '100%' }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: `var(--roadmap-label-width) ${range.timelineWidth}px`, minHeight: 35, borderTop: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Box data-roadmap-label sx={{ position: 'sticky', left: 0, top: 0, zIndex: 6, px: 2.25, py: 1, bgcolor: 'background.paper', borderRight: 1, borderColor: 'divider' }}>
            <Typography variant="caption" color="text.secondary" sx={{ fontSize: 9, fontWeight: 730, letterSpacing: '.06em' }}>WORK ITEM</Typography>
          </Box>
          <Box sx={{ position: 'sticky', top: 0, zIndex: 5, height: 35, bgcolor: 'background.paper' }}>
            {ticks.map((tick) => {
              const x = dateToX(tick, range);
              return <Box key={tick} sx={{ position: 'absolute', left: x, top: 0, bottom: 0, borderLeft: 1, borderColor: 'divider' }}><Typography variant="caption" color="text.secondary" sx={{ position: 'absolute', top: 9, left: 6, whiteSpace: 'nowrap', fontSize: 9 }}>{formatRoadmapDate(tick)}</Typography></Box>;
            })}
            <Box sx={{ position: 'absolute', left: todayX, top: 0, bottom: 0, borderLeft: 1.5, borderColor: 'primary.main' }}><Typography variant="caption" sx={{ position: 'absolute', top: 1, left: 5, color: 'primary.main', fontSize: 8.5, fontWeight: 800 }}>TODAY</Typography></Box>
          </Box>
        </Box>

        {items.map((item) => {
          const bar = layoutRoadmapBar(item.startAt, item.targetAt, item.forecastAt, item.progress.ratio, range);
          const plannedWidth = Math.max(2, bar.plannedEndX - bar.plannedStartX);
          const progressWidth = Math.max(0, bar.progressEndX - bar.progressStartX);
          return <Box
            key={item.id}
            component="article"
            role="button"
            tabIndex={0}
            aria-label={`${item.title}. ${attentionLabels[item.attention]}. ${item.progress.label}. Target ${formatRoadmapDate(item.targetAt)}`}
            onClick={() => openRow(item)}
            onKeyDown={(event) => onRowKeyDown(event, item)}
            sx={{ display: 'grid', gridTemplateColumns: `var(--roadmap-label-width) ${range.timelineWidth}px`, minHeight: 52, borderBottom: 1, borderColor: 'divider', cursor: 'pointer', '&:hover': { bgcolor: 'surface.subtle' }, '&:hover .roadmap-meta': { bgcolor: 'surface.subtle' }, '&:hover .roadmap-progress': { filter: 'saturate(1.12)', opacity: 1 }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: -2 } }}
          >
            <Box className="roadmap-meta" sx={{ position: 'sticky', left: 0, zIndex: 4, minWidth: 0, px: 2.25, py: 0.85, bgcolor: 'background.paper', borderRight: 1, borderColor: 'divider' }}>
              <Typography variant="body2" sx={{ fontWeight: 680 }} noWrap>{item.title}</Typography>
              <Typography variant="caption" sx={{ display: 'block', color: attentionColor[item.attention] }} noWrap>{attentionLabels[item.attention]} · {item.progress.label}</Typography>
            </Box>
            <Box
              onPointerDown={startPan}
              onPointerMove={movePan}
              onPointerUp={finishPan}
              onPointerCancel={finishPan}
              sx={{ position: 'relative', height: 51, cursor: panning ? 'grabbing' : 'grab', userSelect: panning ? 'none' : 'auto' }}
            >
              {ticks.map((tick) => <Box key={tick} sx={{ position: 'absolute', left: dateToX(tick, range), top: 0, bottom: 0, borderLeft: 1, borderColor: 'divider', opacity: 0.62, pointerEvents: 'none' }} />)}
              <Box sx={{ position: 'absolute', left: todayX, top: 0, bottom: 0, zIndex: 1, borderLeft: 1.5, borderColor: 'primary.main', opacity: 0.82, pointerEvents: 'none' }} />

              <Tooltip title={<RoadmapDetailTooltip item={item} />}>
                <Box data-roadmap-interactive component="span" role="img" tabIndex={0} aria-label={`${item.title}, ${item.progress.label}`} sx={{ position: 'absolute', left: bar.plannedStartX, top: 22, width: plannedWidth, height: 10, zIndex: 2, borderRadius: 5, bgcolor: 'surface.secondary', border: 1, borderColor: 'divider', overflow: 'hidden', cursor: 'pointer', '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 } }}>
                  <Box className="roadmap-progress" sx={{ width: progressWidth, maxWidth: '100%', height: '100%', borderRadius: 5, bgcolor: item.attention === 'high' ? 'primary.main' : item.attention === 'attention' ? 'primary.main' : 'success.main', opacity: item.attention === 'normal' || item.attention === 'low' ? 0.78 : 0.9, transition: 'filter 120ms ease, opacity 120ms ease' }} />
                </Box>
              </Tooltip>

              {bar.continuesLeft && <Box aria-hidden sx={{ position: 'absolute', left: 2, top: 24, zIndex: 3, width: 0, height: 0, borderTop: '3px solid transparent', borderBottom: '3px solid transparent', borderRight: '5px solid', borderRightColor: 'text.secondary' }} />}
              {bar.continuesRight && <Box aria-hidden sx={{ position: 'absolute', right: 2, top: 24, zIndex: 3, width: 0, height: 0, borderTop: '3px solid transparent', borderBottom: '3px solid transparent', borderLeft: '5px solid', borderLeftColor: 'text.secondary' }} />}

              {bar.targetVisible && <Tooltip title={`Target · ${formatRoadmapDate(item.targetAt, true)}`}>
                <Box data-roadmap-interactive component="span" role="img" tabIndex={0} aria-label={`Target ${formatRoadmapDate(item.targetAt, true)}`} sx={{ position: 'absolute', left: bar.targetX, top: 21, zIndex: 4, width: 10, height: 10, transform: 'translateX(-50%) rotate(45deg)', bgcolor: 'background.paper', border: 1.7, borderColor: 'text.primary', cursor: 'pointer', '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 } }} />
              </Tooltip>}

              {bar.forecastLate && bar.forecastX !== undefined && <>
                <Box sx={{ position: 'absolute', left: bar.targetX, top: 27, width: Math.max(0, bar.forecastX - bar.targetX), zIndex: 3, borderTop: '1.5px dashed', borderColor: item.attention === 'high' ? 'error.main' : 'warning.main', pointerEvents: 'none' }} />
                {bar.forecastVisible && <Tooltip title={`Forecast · ${formatRoadmapDate(item.forecastAt as string, true)} · +${differenceInRoadmapDays(item.targetAt, item.forecastAt as string)} days`}>
                  <Box data-roadmap-interactive component="span" role="img" tabIndex={0} aria-label={`Forecast ${formatRoadmapDate(item.forecastAt as string, true)}`} sx={{ position: 'absolute', left: bar.forecastX, top: 22.5, zIndex: 4, width: 9, height: 9, transform: 'translateX(-50%) rotate(45deg)', bgcolor: 'background.paper', border: 1.5, borderColor: item.attention === 'high' ? 'error.main' : 'warning.main', cursor: 'pointer', '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 } }} />
                </Tooltip>}
              </>}

              {item.milestones.filter((milestone) => dateToX(milestone.occurredAt, range) >= 0 && dateToX(milestone.occurredAt, range) <= range.timelineWidth).map((milestone) => {
                const x = dateToX(milestone.occurredAt, range);
                return <Tooltip key={milestone.id} title={`${milestone.title} · ${formatRoadmapDate(milestone.occurredAt, true)} · ${milestone.status}`}>
                  <Box data-roadmap-interactive component="span" role="img" tabIndex={0} aria-label={`${milestone.title}, ${formatRoadmapDate(milestone.occurredAt, true)}, ${milestone.status}`} sx={{ position: 'absolute', left: x, top: 11, zIndex: 4, width: 6, height: 6, transform: 'translateX(-50%)', borderRadius: '50%', bgcolor: milestone.status === 'completed' ? 'success.main' : 'background.paper', border: 1.2, borderColor: milestone.status === 'completed' ? 'success.main' : 'text.secondary', cursor: 'pointer', '&::after': { content: '""', position: 'absolute', left: '50%', top: 6, height: 5, borderLeft: 1, borderColor: 'divider' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 } }} />
                </Tooltip>;
              })}
            </Box>
          </Box>;
        })}
      </Box>
    </Box>
  </Box>;
}
