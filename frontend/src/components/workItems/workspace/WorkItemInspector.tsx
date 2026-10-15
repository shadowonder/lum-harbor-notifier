import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import InfoOutlined from '@mui/icons-material/InfoOutlined';
import {
  Box,
  Button,
  ButtonBase,
  Divider,
  IconButton,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import type { ReactNode } from 'react';
import { getHealthColor, getItemTypeIcon, getItemTypeLabel } from '../workflow/workflowLayout.ts';
import {
  hasWorkflowData,
  WORKFLOW_STAGES,
  type WorkflowModel,
  type WorkflowNode,
} from '../workflow/workflowTypes.ts';
import { getItemRelationships, getItemSignal } from './itemExplorer.ts';

interface WorkItemInspectorProps {
  node: WorkflowNode | null;
  model: WorkflowModel;
  mode?: 'compact' | 'detail';
  embedded?: boolean;
  onClearSelection: () => void;
  onSelectNode: (id: string) => void;
  onViewDetails?: (id: string) => void;
}

function Label({ children }: { children: ReactNode }) {
  return (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{ display: 'block', fontWeight: 700, letterSpacing: '0.04em' }}
    >
      {children}
    </Typography>
  );
}

function RelationshipRows({
  title,
  nodes,
  onSelect,
}: {
  title: string;
  nodes: WorkflowNode[];
  onSelect: (id: string) => void;
}) {
  return (
    <Box>
      <Label>
        {title} ({nodes.length})
      </Label>
      {nodes.length ? (
        nodes.map((related) => (
          <ButtonBase
            key={related.id}
            component="div"
            onClick={() => onSelect(related.id)}
            sx={{
              width: '100%',
              py: 0.65,
              justifyContent: 'space-between',
              gap: 1,
              textAlign: 'left',
              borderBottom: 1,
              borderColor: 'divider',
              '&:last-child': { borderBottom: 0 },
              '&:hover': { color: 'primary.main' },
            }}
          >
            <Stack direction="row" spacing={0.7} sx={{ alignItems: 'center', minWidth: 0 }}>
              <Typography aria-hidden>{getItemTypeIcon(related.itemType)}</Typography>
              <Typography variant="body2" noWrap>
                {related.title}
              </Typography>
            </Stack>
            <Typography variant="caption" color="text.secondary">
              {related.stageId.toUpperCase()}
            </Typography>
          </ButtonBase>
        ))
      ) : (
        <Typography variant="caption" color="text.secondary">
          None
        </Typography>
      )}
    </Box>
  );
}

export default function WorkItemInspector({
  node,
  model,
  mode = 'detail',
  embedded = false,
  onClearSelection,
  onSelectNode,
  onViewDetails,
}: WorkItemInspectorProps) {
  const theme = useTheme();
  const workflowAvailable = hasWorkflowData(model);

  if (!node) {
    const empty = (
      <Stack
        direction={{ xs: 'column', md: 'row' }}
        spacing={1.5}
        sx={{ p: 2, alignItems: { md: 'center' }, justifyContent: 'space-between' }}
      >
        <Box>
          <Typography variant="h3">No item selected</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35 }}>
            Select an item to inspect its timing, dependency chain, related work, evidence, and item
            history.
          </Typography>
        </Box>
        <Stack
          direction="row"
          spacing={0.8}
          sx={{ alignItems: 'center', color: 'text.secondary', flex: '0 0 auto' }}
        >
          <InfoOutlined sx={{ fontSize: 17 }} />
          <Typography variant="caption">
            {workflowAvailable ? `Workflow: ${model.trunk.label}` : 'Workflow data unavailable'}
          </Typography>
        </Stack>
      </Stack>
    );
    return embedded ? <Box>{empty}</Box> : <Paper variant="outlined">{empty}</Paper>;
  }

  const currentStage = WORKFLOW_STAGES.find((stage) => stage.id === node.stageId);
  const targetStage = node.targetStageId
    ? WORKFLOW_STAGES.find((stage) => stage.id === node.targetStageId)
    : null;
  const { dependsOn, blocks } = getItemRelationships(model, node.id);
  const itemEvents = model.itemHistories?.[node.id] ?? [];
  const color = getHealthColor(node.health, theme.palette.mode === 'dark');

  if (mode === 'compact') {
    return (
      <Paper variant="outlined" sx={{ px: 2, py: 1.5 }}>
        <Stack
          direction={{ xs: 'column', md: 'row' }}
          spacing={1.5}
          sx={{ alignItems: { md: 'center' }, justifyContent: 'space-between' }}
        >
          <Box sx={{ minWidth: 0 }}>
            <Stack direction="row" spacing={0.8} sx={{ alignItems: 'center' }}>
              <Box aria-hidden sx={{ color, fontSize: 17, fontWeight: 700 }}>
                {getItemTypeIcon(node.itemType)}
              </Box>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
                {getItemTypeLabel(node.itemType)} · {node.health.toUpperCase()} ·{' '}
                {currentStage?.label ?? node.stageId}
              </Typography>
            </Stack>
            <Typography variant="h3" sx={{ mt: 0.3 }}>
              {node.title}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35 }}>
              {getItemSignal(node, model)} · {node.statusReason ?? node.summary}
            </Typography>
          </Box>
          <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', flex: '0 0 auto' }}>
            <Button
              size="small"
              endIcon={<ArrowForwardRounded />}
              onClick={() => onViewDetails?.(node.id)}
            >
              View {node.title} details
            </Button>
            <IconButton size="small" onClick={onClearSelection} aria-label="Clear node selection">
              <CloseRounded fontSize="small" />
            </IconButton>
          </Stack>
        </Stack>
      </Paper>
    );
  }

  const content = (
    <Stack spacing={2} sx={{ p: 2, minWidth: 0 }}>
      <Box component="section" aria-label="Item identity">
        <Stack direction="row" spacing={0.8} sx={{ alignItems: 'center' }}>
          <Box aria-hidden sx={{ color, fontSize: 19, fontWeight: 700 }}>
            {getItemTypeIcon(node.itemType)}
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
            {getItemTypeLabel(node.itemType).toUpperCase()} ·{' '}
            {node.health === 'yellow'
              ? 'AT RISK'
              : node.health === 'red'
                ? 'BLOCKED'
                : node.health === 'green'
                  ? 'ON TRACK'
                  : 'UNKNOWN'}
          </Typography>
        </Stack>
        <Typography variant="h2" sx={{ mt: 0.4 }}>
          {node.title}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.55 }}>
          {node.description ?? node.summary ?? 'No description configured for this item.'}
        </Typography>
      </Box>

      <Divider />
      <Box component="section" aria-label="Current state and timing">
        <Label>CURRENT STATE & TIMING</Label>
        <Box
          sx={{
            mt: 0.65,
            display: 'grid',
            gridTemplateColumns: {
              xs: 'repeat(2, minmax(0, 1fr))',
              xl: 'repeat(4, minmax(0, 1fr))',
            },
            gap: 1.5,
          }}
        >
          <Box>
            <Typography variant="caption" color="text.secondary">
              Current
            </Typography>
            <Typography variant="body2" sx={{ fontWeight: 700 }}>
              {currentStage?.label.toUpperCase() ?? node.stageId.toUpperCase()}
            </Typography>
          </Box>
          <Box>
            <Typography variant="caption" color="text.secondary">
              Target
            </Typography>
            <Typography variant="body2" sx={{ fontWeight: 700 }}>
              {targetStage
                ? `${targetStage.label.toUpperCase()}${node.targetDate ? ` · ${node.targetDate}` : ''}`
                : (node.targetDate ?? 'Not configured')}
            </Typography>
          </Box>
          <Box>
            <Typography variant="caption" color="text.secondary">
              Next
            </Typography>
            <Typography variant="body2" sx={{ fontWeight: 700 }}>
              {node.nextMilestone?.label ?? 'Not available'}
            </Typography>
          </Box>
          <Box>
            <Typography variant="caption" color="text.secondary">
              Time
            </Typography>
            <Typography
              variant="body2"
              color={
                node.nextMilestone?.timingLabel.toLowerCase().includes('overdue')
                  ? 'error.main'
                  : 'text.primary'
              }
              sx={{ fontWeight: 700 }}
            >
              {node.nextMilestone?.timingLabel ?? 'Not available'}
            </Typography>
            {node.nextMilestone && (
              <Typography variant="caption" color="text.secondary">
                {node.nextMilestone.targetAt}
              </Typography>
            )}
          </Box>
        </Box>
      </Box>

      <Divider />
      <Box component="section" aria-label="Item dependencies">
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' },
            gap: 2,
          }}
        >
          <RelationshipRows title="DEPENDS ON" nodes={dependsOn} onSelect={onSelectNode} />
          <RelationshipRows title="BLOCKS" nodes={blocks} onSelect={onSelectNode} />
        </Box>
      </Box>

      <Divider />
      <Box component="section" aria-label="Related work">
        <Label>RELATED WORK</Label>
        {node.relatedTodos?.length || node.relatedNotifications?.length ? (
          <Box sx={{ mt: 0.4 }}>
            {node.relatedTodos?.map((todo) => (
              <Stack
                key={todo.id}
                direction="row"
                sx={{
                  py: 0.7,
                  justifyContent: 'space-between',
                  gap: 1,
                  borderBottom: 1,
                  borderColor: 'divider',
                }}
              >
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
                    TODO
                  </Typography>
                  <Typography variant="body2">{todo.title}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {todo.timing}
                  </Typography>
                </Box>
                <Typography
                  variant="caption"
                  color={
                    todo.status === 'BLOCKED'
                      ? 'error.main'
                      : todo.status === 'COMPLETED'
                        ? 'success.main'
                        : 'text.secondary'
                  }
                >
                  {todo.status}
                </Typography>
              </Stack>
            ))}
            {node.relatedNotifications?.map((notification) => (
              <Stack
                key={notification.id}
                direction="row"
                sx={{
                  py: 0.7,
                  justifyContent: 'space-between',
                  gap: 1,
                  borderBottom: 1,
                  borderColor: 'divider',
                }}
              >
                <Box>
                  <Typography
                    variant="caption"
                    color={notification.severity === 'CRITICAL' ? 'error.main' : 'warning.main'}
                    sx={{ fontWeight: 700 }}
                  >
                    {notification.severity === 'CRITICAL' ? 'ALERT' : 'NOTIFICATION'}
                  </Typography>
                  <Typography variant="body2">{notification.title}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {notification.timing}
                  </Typography>
                </Box>
                <Typography variant="caption" color="text.secondary">
                  {notification.status}
                </Typography>
              </Stack>
            ))}
          </Box>
        ) : (
          <Typography variant="caption" color="text.secondary">
            No related todos, notifications, or alerts.
          </Typography>
        )}
      </Box>

      <Divider />
      <Box component="section" aria-label="Source and evidence">
        <Label>SOURCE / EVIDENCE</Label>
        <Typography variant="body2" sx={{ mt: 0.4, fontWeight: 700 }}>
          {node.source?.label ?? 'Not available'}
        </Typography>
        {node.source && (
          <Stack spacing={0.2} sx={{ mt: 0.35 }}>
            <Typography variant="caption" color="text.secondary">
              {node.source.kind}
              {node.source.reference ? ` · ${node.source.reference}` : ''}
            </Typography>
            {node.source.observedAt && (
              <Typography variant="caption" color="text.secondary">
                Observed / created: {node.source.observedAt}
              </Typography>
            )}
            {node.source.why && (
              <Typography variant="caption" color="text.secondary">
                Why this exists: {node.source.why}
              </Typography>
            )}
            {node.source.detail && (
              <Typography variant="caption" color="text.secondary">
                {node.source.detail}
              </Typography>
            )}
            {node.evidence && (
              <Typography variant="caption" color="text.secondary">
                Evidence: {node.evidence}
              </Typography>
            )}
          </Stack>
        )}
      </Box>

      <Divider />
      <Box component="section" aria-label="Item history">
        <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'baseline' }}>
          <Label>ITEM ACTIVITY</Label>
          <Typography variant="caption" color="text.secondary">
            {itemEvents.length} item-specific events
          </Typography>
        </Stack>
        {itemEvents.length ? (
          itemEvents.map((event) => (
            <Box
              key={event.id}
              sx={{
                display: 'grid',
                gridTemplateColumns: { xs: '86px minmax(0, 1fr)', sm: '112px minmax(0, 1fr)' },
                gap: 1.25,
                py: 0.9,
                borderBottom: 1,
                borderColor: 'divider',
                '&:last-child': { borderBottom: 0 },
              }}
            >
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontVariantNumeric: 'tabular-nums' }}
              >
                {event.timestamp}
              </Typography>
              <Box>
                <Typography variant="body2" sx={{ fontWeight: 650 }}>
                  {event.title}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {event.description}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Source: {event.source ?? event.actor ?? 'Unknown'}
                  {event.reason ? ` · Why: ${event.reason}` : ''}
                  {event.evidence ? ` · Evidence: ${event.evidence}` : ''}
                </Typography>
              </Box>
            </Box>
          ))
        ) : (
          <Typography variant="caption" color="text.secondary">
            No item-specific history is available.
          </Typography>
        )}
      </Box>
    </Stack>
  );

  return embedded ? <Box>{content}</Box> : <Paper variant="outlined">{content}</Paper>;
}
