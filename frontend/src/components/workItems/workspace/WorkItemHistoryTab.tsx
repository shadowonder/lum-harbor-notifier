import { useMemo, useState } from 'react';
import AccountTreeRounded from '@mui/icons-material/AccountTreeRounded';
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded';
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import FlagRounded from '@mui/icons-material/FlagRounded';
import SwapHorizRounded from '@mui/icons-material/SwapHorizRounded';
import TrendingUpRounded from '@mui/icons-material/TrendingUpRounded';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import {
  Box,
  Button,
  ButtonBase,
  Chip,
  Collapse,
  Divider,
  Paper,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import type { ProductWorkItem } from '../../../api/workItems/productApi.ts';
import type { WorkflowHistoryEvent, WorkflowModel } from '../workflow/workflowTypes.ts';

interface WorkItemHistoryTabProps {
  item: ProductWorkItem;
  model: WorkflowModel;
  selectedNodeId?: string | null;
  onSelectNode?: (nodeId: string | null) => void;
}

function formatDateTime(value: string) {
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value));
  } catch {
    return value;
  }
}

function getEventIcon(type: WorkflowHistoryEvent['type']) {
  switch (type) {
    case 'alert':
      return <ErrorOutlineRounded fontSize="small" sx={{ color: 'error.main' }} />;
    case 'milestone':
      return <FlagRounded fontSize="small" sx={{ color: 'primary.main' }} />;
    case 'transition':
      return <SwapHorizRounded fontSize="small" sx={{ color: 'secondary.main' }} />;
    case 'health_change':
      return <TrendingUpRounded fontSize="small" sx={{ color: 'warning.main' }} />;
    case 'dependency':
    default:
      return <AccountTreeRounded fontSize="small" sx={{ color: 'info.main' }} />;
  }
}

function getEventBadgeColor(
  type: WorkflowHistoryEvent['type'],
): 'error' | 'primary' | 'secondary' | 'warning' | 'info' {
  switch (type) {
    case 'alert':
      return 'error';
    case 'milestone':
      return 'primary';
    case 'transition':
      return 'secondary';
    case 'health_change':
      return 'warning';
    case 'dependency':
    default:
      return 'info';
  }
}

export default function WorkItemHistoryTab({
  item,
  model,
  selectedNodeId = null,
  onSelectNode,
}: WorkItemHistoryTabProps) {
  const [typeFilter, setTypeFilter] = useState<string>('all');
  const [filterNodeId, setFilterNodeId] = useState<string | null>(selectedNodeId);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  // Synchronize when parent changes selectedNodeId
  const activeNodeFilter = filterNodeId;

  // Workflow History remains conceptually separate from selected-item history.
  const allEvents = useMemo(() => {
    return [...(model.historyEvents ?? [])];
  }, [model.historyEvents]);

  // Filter events based on active filters
  const filteredEvents = useMemo(() => {
    return allEvents.filter((ev) => {
      // Node filter
      if (activeNodeFilter && ev.nodeId !== activeNodeFilter) {
        return false;
      }
      // Type filter
      if (typeFilter !== 'all' && ev.type !== typeFilter) {
        return false;
      }
      return true;
    });
  }, [allEvents, activeNodeFilter, typeFilter]);

  const focusedNode = activeNodeFilter ? model.nodes.find((n) => n.id === activeNodeFilter) : null;

  return (
    <Stack spacing={2}>
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Box
          sx={{
            px: 1.25,
            py: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 1.5,
            flexWrap: 'wrap',
            bgcolor: 'surface.subtle',
          }}
        >
          <Box>
            <Typography variant="h2">Full History</Typography>
            <Typography variant="caption" color="text.secondary">
              Complete workflow timeline: creation, structure, dependencies, targets, health, gates,
              related work, notifications, and evidence.
            </Typography>
          </Box>
          <Typography variant="caption" color="text.secondary">
            Showing {filteredEvents.length} of {allEvents.length} events
          </Typography>
        </Box>
        <Divider />
        <Box
          sx={{
            px: 1.25,
            py: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 1,
            flexWrap: 'wrap',
            bgcolor: 'surface.subtle',
          }}
        >
          <ToggleButtonGroup
            exclusive
            size="small"
            value={activeNodeFilter ? 'selected' : 'all'}
            onChange={(_, value) => value === 'all' && setFilterNodeId(null)}
            aria-label="History scope"
          >
            <ToggleButton value="all">All Events ({allEvents.length})</ToggleButton>
            {focusedNode && <ToggleButton value="selected">Selected Item</ToggleButton>}
          </ToggleButtonGroup>
          <ToggleButtonGroup
            exclusive
            size="small"
            value={typeFilter}
            onChange={(_, value) => value && setTypeFilter(value)}
            aria-label="History event type"
          >
            <ToggleButton value="all">All</ToggleButton>
            <ToggleButton value="alert">Alerts</ToggleButton>
            <ToggleButton value="milestone">Milestones</ToggleButton>
            <ToggleButton value="transition">Transitions</ToggleButton>
            <ToggleButton value="health_change">Health</ToggleButton>
          </ToggleButtonGroup>
        </Box>
        <Divider />

        {filteredEvents.length === 0 ? (
          <Box sx={{ py: 7, textAlign: 'center' }}>
            <Typography color="text.secondary">No events match the selected filter.</Typography>
          </Box>
        ) : (
          filteredEvents.map((ev) => {
            const linkedNode = ev.nodeId ? model.nodes.find((node) => node.id === ev.nodeId) : null;
            const expanded = expandedId === ev.id;
            return (
              <Box
                key={ev.id}
                sx={{
                  borderBottom: 1,
                  borderColor: 'divider',
                  '&:last-child': { borderBottom: 0 },
                }}
              >
                <ButtonBase
                  onClick={() => setExpandedId(expanded ? null : ev.id)}
                  aria-expanded={expanded}
                  sx={{
                    width: '100%',
                    px: 2,
                    py: 1.15,
                    textAlign: 'left',
                    alignItems: 'flex-start',
                    '&:hover': { bgcolor: 'action.hover' },
                  }}
                >
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{
                      width: 132,
                      flex: '0 0 auto',
                      pt: 0.25,
                      fontVariantNumeric: 'tabular-nums',
                    }}
                  >
                    {ev.timestamp}
                  </Typography>
                  <Box
                    sx={{
                      width: 26,
                      height: 26,
                      mr: 1.25,
                      flex: '0 0 auto',
                      display: 'grid',
                      placeItems: 'center',
                      borderRadius: 1,
                      bgcolor: 'surface.secondary',
                    }}
                  >
                    {getEventIcon(ev.type)}
                  </Box>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Stack
                      direction="row"
                      spacing={0.8}
                      useFlexGap
                      sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                    >
                      <Typography sx={{ fontWeight: 650 }}>{ev.title}</Typography>
                      <Chip
                        size="small"
                        label={ev.type.replace('_', ' ')}
                        variant="outlined"
                        color={getEventBadgeColor(ev.type)}
                        sx={{ height: 20, '& .MuiChip-label': { px: 0.75, fontSize: 10.5 } }}
                      />
                    </Stack>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 0.2 }}>
                      {ev.description}
                    </Typography>
                  </Box>
                  <ExpandMoreRounded
                    sx={{
                      ml: 1,
                      mt: 0.15,
                      fontSize: 18,
                      color: 'text.secondary',
                      transform: expanded ? 'rotate(180deg)' : 'none',
                      transition: 'transform 140ms ease',
                    }}
                  />
                </ButtonBase>
                <Collapse in={expanded} unmountOnExit>
                  <Box
                    sx={{
                      ml: { xs: 2, sm: '172px' },
                      mr: 2,
                      mb: 1.5,
                      p: 1.5,
                      border: 1,
                      borderColor: 'divider',
                      borderRadius: 1,
                      bgcolor: 'surface.subtle',
                    }}
                  >
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ display: 'block', fontWeight: 650 }}
                    >
                      SOURCE / EXPLANATION
                    </Typography>
                    <Typography variant="body2">{ev.source ?? ev.actor ?? 'Lum Harbor'}</Typography>
                    {ev.reason && (
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                        Why: {ev.reason}
                      </Typography>
                    )}
                    {ev.affectedEntity && (
                      <Typography variant="body2" color="text.secondary">
                        Affected: {ev.affectedEntity}
                      </Typography>
                    )}
                    {ev.evidence && (
                      <Typography variant="body2" color="text.secondary">
                        Evidence: {ev.evidence}
                      </Typography>
                    )}
                    {linkedNode && (
                      <Button
                        size="small"
                        sx={{ mt: 1 }}
                        onClick={() => {
                          setFilterNodeId(linkedNode.id);
                          onSelectNode?.(linkedNode.id);
                        }}
                      >
                        Focus branch: {linkedNode.title}
                      </Button>
                    )}
                  </Box>
                </Collapse>
              </Box>
            );
          })
        )}
      </Paper>

      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Box
          sx={{
            px: 2,
            py: 0.8,
            borderBottom: 1,
            borderColor: 'divider',
            bgcolor: 'surface.subtle',
          }}
        >
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ fontWeight: 700, letterSpacing: '0.08em' }}
          >
            AUTHORITATIVE DATABASE TIMESTAMPS
          </Typography>
        </Box>
        <Box sx={{ p: 1.5 }}>
          <Stack spacing={1}>
            <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
              <Typography
                variant="caption"
                sx={{ width: 160, color: 'text.secondary', fontWeight: 600 }}
              >
                {formatDateTime(item.createdAt)}
              </Typography>
              <Typography variant="body2">
                Outcome created in database · Initial Lifecycle: <strong>ACTIVE</strong>
              </Typography>
            </Stack>

            {item.updatedAt !== item.createdAt && (
              <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
                <Typography
                  variant="caption"
                  sx={{ width: 160, color: 'text.secondary', fontWeight: 600 }}
                >
                  {formatDateTime(item.updatedAt)}
                </Typography>
                <Typography variant="body2">
                  Last committed mutation · Current Lifecycle:{' '}
                  <strong>{item.lifecycleState}</strong>
                </Typography>
              </Stack>
            )}

            {item.completedAt && (
              <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
                <Typography
                  variant="caption"
                  sx={{ width: 160, color: 'text.secondary', fontWeight: 600 }}
                >
                  {formatDateTime(item.completedAt)}
                </Typography>
                <Typography variant="body2" sx={{ color: 'success.main', fontWeight: 650 }}>
                  <CheckCircleOutlineRounded
                    sx={{ fontSize: '0.9rem', verticalAlign: 'text-bottom', mr: 0.5 }}
                  />
                  Transitioned to COMPLETED
                </Typography>
              </Stack>
            )}
          </Stack>
        </Box>
      </Paper>
    </Stack>
  );
}
