import { useState } from 'react';
import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import ScienceRounded from '@mui/icons-material/ScienceRounded';
import EventNoteRounded from '@mui/icons-material/EventNoteRounded';
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Snackbar,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import type {
  ProductWorkItem,
  ProductWorkItemLifecycle,
} from '../../../api/workItems/productApi.ts';
import type { ProductProposal } from '../../../api/proposals/proposalProductApi.ts';
import type { ProductTrackingApi } from '../../../api/tracking/trackingProductApi.ts';
import { productTrackingApi } from '../../../api/tracking/wailsTrackingProductApi.ts';
import { createLocalTodo } from '../../../api/plannerApi.ts';
import { useWorkItemTracking } from '../../../tracking/useWorkItemTracking.ts';
import {
  TrackedItemsPanel,
  TrackingStateNotice,
  TrackingWorkflowPanel,
  type TrackingViewProps,
} from '../tracking/TrackingPanels.tsx';
import {
  describeTiming,
  findActiveFinalTarget,
  formatMonitorDate,
} from '../../../tracking/trackingPresentation.ts';
import {
  workItemLifecycleActions,
  type WorkItemsPageState,
} from '../../../pages/workItemsPageState.ts';
import {
  buildWorkflowModelFromWorkItem,
  hasWorkflowData,
  type WorkflowModel,
} from '../workflow/workflowTypes.ts';
import WorkItemHistoryTab from './WorkItemHistoryTab.tsx';
import WorkItemScopedHistory from './WorkItemScopedHistory.tsx';
import WorkItemItemsTab from './WorkItemItemsTab.tsx';
import WorkItemOverviewTab from './WorkItemOverviewTab.tsx';
import { createItemExplorerDeepLink } from './itemExplorer.ts';
import {
  defaultScheduleActionDueDate,
  reminderOffsetsForPolicy,
  scheduleActionDue,
  scheduleActionWorkflowStep,
  type ScheduleReminderPolicy,
} from './scheduleAction.ts';
import { projectWorkItemGhost } from '../../../proposals/workItemGhostProjection.ts';

interface WorkItemWorkspaceProps {
  item: ProductWorkItem;
  state: WorkItemsPageState;
  onBack: () => void;
  onChangeLifecycle: (target: ProductWorkItemLifecycle) => Promise<void>;
  /** Fixture/prototype rendering only. Production omits it and uses real Tracking truth. */
  modelOverride?: WorkflowModel;
  trackingApi?: ProductTrackingApi;
  activeProposal?: ProductProposal | null;
  onOpenProposalTestTool?: () => void;
}

export default function WorkItemWorkspace({
  item,
  state,
  onBack,
  onChangeLifecycle,
  modelOverride,
  trackingApi = productTrackingApi,
  activeProposal = null,
  onOpenProposalTestTool,
}: WorkItemWorkspaceProps) {
  const [activeTab, setActiveTab] = useState<'workflow' | 'items' | 'history'>('workflow');
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  const realTracking = !modelOverride;
  const tracking = useWorkItemTracking(trackingApi, realTracking ? item.id : null);

  const model = modelOverride ?? buildWorkflowModelFromWorkItem(item);
  const workflowAvailable = hasWorkflowData(model);
  const actions = workItemLifecycleActions(item.lifecycleState);
  const preview = projectWorkItemGhost(item, activeProposal);

  const trackingView: TrackingViewProps =
    tracking.view.status === 'error'
      ? { status: 'error', message: tracking.view.message, onRetry: () => void tracking.reload() }
      : tracking.view.status === 'ready'
        ? { status: 'ready', data: tracking.view.data }
        : { status: tracking.view.status };
  const trackingData = tracking.view.status === 'ready' ? tracking.view.data : null;
  const now = new Date();
  const finalTarget = trackingData ? findActiveFinalTarget(trackingData) : null;

  const [scheduleDialogOpen, setScheduleDialogOpen] = useState(false);
  const [todoTitle, setTodoTitle] = useState('');
  const [todoDueDate, setTodoDueDate] = useState('');
  const [todoDueTime, setTodoDueTime] = useState('');
  const [todoTrackedItemId, setTodoTrackedItemId] = useState('');
  const [todoReminderPolicy, setTodoReminderPolicy] = useState<ScheduleReminderPolicy>('standard');
  const [scheduleBusy, setScheduleBusy] = useState(false);
  const [scheduleSuccess, setScheduleSuccess] = useState<string | null>(null);
  const [scheduleError, setScheduleError] = useState<string | null>(null);

  const handleOpenScheduleDialog = () => {
    const currentStep = scheduleActionWorkflowStep(trackingData);
    const defaultTitle = currentStep
      ? `${item.title}: ${currentStep.title}`
      : `${item.title}: Action`;
    setTodoTitle(defaultTitle);
    setTodoDueDate(defaultScheduleActionDueDate());
    setTodoDueTime('');
    setTodoTrackedItemId(
      trackingData?.trackedItems.find((trackedItem) => trackedItem.lifecycleState === 'ACTIVE')?.id ?? '',
    );
    setTodoReminderPolicy('standard');
    setScheduleError(null);
    setScheduleDialogOpen(true);
  };

  const handleCreateScheduleAction = async () => {
    if (!todoTitle.trim()) return;
    setScheduleBusy(true);
    setScheduleError(null);
    try {
      const offsetsSeconds = reminderOffsetsForPolicy(todoReminderPolicy);
      await createLocalTodo({
        title: todoTitle.trim(),
        ...scheduleActionDue(todoDueDate, todoDueTime),
        workItemId: item.id,
        workflowId: trackingData?.workflow?.id || undefined,
        workflowStepId: scheduleActionWorkflowStep(trackingData)?.id,
        trackedItemId: todoTrackedItemId || undefined,
        offsetsSeconds: offsetsSeconds,
      });
      setScheduleDialogOpen(false);
      setScheduleSuccess('Action scheduled in Planner');
    } catch (err: unknown) {
      setScheduleError(err instanceof Error ? err.message : String(err));
    } finally {
      setScheduleBusy(false);
    }
  };

  return (
    <Stack spacing={2}>
      {/* 1. Breadcrumb & Back Navigation */}
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
        <Button
          size="small"
          startIcon={<ArrowBackRounded />}
          onClick={onBack}
          disabled={state.lifecycleSaving}
          sx={{ textTransform: 'none', fontWeight: 600 }}
        >
          All Work Items
        </Button>
        <Typography color="text.secondary">·</Typography>
        <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 600 }}>
          {item.id}
        </Typography>
      </Stack>

      <Box
        component="header"
        sx={{
          pb: 2,
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        <Stack spacing={2}>
          {/* Status alerts */}
          {state.lifecycleError && <Alert severity="error">{state.lifecycleError}</Alert>}
          {!preview.supported && (
            <Alert severity="warning">
              Preview is unavailable for this proposal operation. Authoritative WorkItem values are
              shown.
            </Alert>
          )}

          {/* Top Title & Primary Actions */}
          <Stack
            direction={{ xs: 'column', md: 'row' }}
            spacing={2}
            sx={{ justifyContent: 'space-between', alignItems: { md: 'flex-start' } }}
          >
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1 }}>
                <Chip
                  size="small"
                  label={item.lifecycleState}
                  variant="outlined"
                  color={
                    item.lifecycleState === 'COMPLETED'
                      ? 'success'
                      : item.lifecycleState === 'PAUSED'
                        ? 'warning'
                        : 'primary'
                  }
                  sx={{ height: 20, '& .MuiChip-label': { px: 0.75, fontSize: 10.5 } }}
                />
                {!realTracking && (
                  <Chip
                    size="small"
                    label={
                      model.hasAuthoritativeHealth
                        ? `HEALTH: ${model.healthLabel}`
                        : 'HEALTH UNKNOWN'
                    }
                    color={
                      model.hasAuthoritativeHealth
                        ? model.aggregateHealth === 'green'
                          ? 'success'
                          : model.aggregateHealth === 'yellow'
                            ? 'warning'
                            : 'error'
                        : 'default'
                    }
                    variant="outlined"
                    sx={{ height: 20, '& .MuiChip-label': { px: 0.75, fontSize: 10.5 } }}
                  />
                )}
              </Stack>

              <Box
                data-testid="work-item-title-preview"
                data-proposed={preview.title.isProposed ? 'true' : 'false'}
                sx={
                  preview.title.isProposed
                    ? {
                        display: 'inline-flex',
                        maxWidth: '100%',
                        alignItems: 'center',
                        gap: 1,
                        px: 1,
                        py: 0.65,
                        ml: -1,
                        border: '1px dashed',
                        borderColor: '#7969C7',
                        borderRadius: 1,
                        bgcolor: 'rgba(121, 105, 199, 0.08)',
                      }
                    : undefined
                }
              >
                <Typography component="h1" variant="h1">
                  {preview.title.value}
                </Typography>
                {preview.title.isProposed && (
                  <Chip
                    size="small"
                    label="PROPOSED"
                    variant="outlined"
                    sx={{ height: 20, color: '#7969C7', borderColor: 'currentColor' }}
                  />
                )}
              </Box>

              <Box
                data-testid="work-item-description-preview"
                data-proposed={preview.description.isProposed ? 'true' : 'false'}
                sx={
                  preview.description.isProposed
                    ? {
                        width: 'fit-content',
                        maxWidth: 780,
                        mt: 0.75,
                        px: 1,
                        py: 0.75,
                        ml: -1,
                        border: '1px dashed',
                        borderColor: '#7969C7',
                        borderRadius: 1,
                        bgcolor: 'rgba(121, 105, 199, 0.08)',
                      }
                    : { mt: 0.75 }
                }
              >
                {preview.description.isProposed && (
                  <Chip
                    size="small"
                    label="PROPOSED"
                    variant="outlined"
                    sx={{ mb: 0.6, height: 20, color: '#7969C7', borderColor: 'currentColor' }}
                  />
                )}
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ whiteSpace: 'pre-wrap', maxWidth: 780 }}
                >
                  {preview.description.value || 'No description provided for this outcome.'}
                </Typography>
              </Box>
            </Box>

            {/* Header Action Buttons */}
            <Stack
              direction="row"
              spacing={1}
              useFlexGap
              sx={{ flexWrap: 'wrap', alignItems: 'center' }}
            >
              {onOpenProposalTestTool && (
                <Button
                  size="small"
                  variant="outlined"
                  color="secondary"
                  startIcon={<ScienceRounded />}
                  onClick={onOpenProposalTestTool}
                  disabled={state.lifecycleSaving}
                >
                  Create Test Proposal (DEV)
                </Button>
              )}

              <Button
                size="small"
                variant="outlined"
                color="primary"
                startIcon={<EventNoteRounded />}
                onClick={handleOpenScheduleDialog}
                disabled={state.lifecycleSaving}
              >
                Schedule Action
              </Button>

              {actions.map((action) => (
                <Button
                  key={action.label}
                  size="small"
                  variant={
                    action.label === 'Complete' ||
                    action.label === 'Resume' ||
                    action.label === 'Reopen'
                      ? 'contained'
                      : 'outlined'
                  }
                  color={action.label === 'Cancel' ? 'error' : 'primary'}
                  disabled={state.lifecycleSaving}
                  onClick={() => void onChangeLifecycle(action.target)}
                >
                  {state.lifecycleSaving ? 'Updating…' : action.label}
                </Button>
              ))}
            </Stack>
          </Stack>

          {/* Metadata counts & timestamps */}
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={{ xs: 1, sm: 3 }}
            sx={{ color: 'text.secondary' }}
          >
            {realTracking ? (
              <>
                <Typography variant="caption">
                  Tracked items:{' '}
                  <strong style={{ color: 'inherit' }}>
                    {trackingData
                      ? trackingData.trackedItems.length
                      : tracking.view.status === 'loading'
                        ? '…'
                        : 'Unavailable'}
                  </strong>
                </Typography>
                <Typography variant="caption">
                  Target:{' '}
                  <strong style={{ color: 'inherit' }}>
                    {trackingData
                      ? finalTarget
                        ? `${formatMonitorDate(finalTarget.dueAt, finalTarget.timezone)} · ${describeTiming(finalTarget, now)}`
                        : 'None configured'
                      : tracking.view.status === 'loading'
                        ? '…'
                        : 'Unavailable'}
                  </strong>
                </Typography>
              </>
            ) : (
              <>
                <Typography variant="caption">
                  Trunk Stage:{' '}
                  <strong style={{ color: 'inherit' }}>
                    {workflowAvailable ? model.trunk.currentStageId.toUpperCase() : 'UNAVAILABLE'}
                  </strong>
                </Typography>
                <Typography variant="caption">
                  Branch Items:{' '}
                  <strong style={{ color: 'inherit' }}>
                    {workflowAvailable ? model.nodes.length : 'Unavailable'}
                  </strong>
                </Typography>
                <Typography variant="caption">
                  Target Date:{' '}
                  <strong style={{ color: 'inherit' }}>
                    {workflowAvailable ? (model.targetDate ?? 'None Configured') : 'Unavailable'}
                  </strong>
                </Typography>
              </>
            )}
            <Typography variant="caption">
              Updated:{' '}
              <strong style={{ color: 'inherit' }}>
                {new Date(item.updatedAt).toLocaleDateString()}
              </strong>
            </Typography>
          </Stack>
        </Stack>
      </Box>

      {/* 2. Workspace Section Tabs */}
      <Box sx={{ pb: 1, borderBottom: 1, borderColor: 'divider', overflow: 'hidden' }}>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={activeTab}
          onChange={(_, val) => {
            if (!val) return;
            if (val === 'items' && !selectedNodeId && model.nodes[0]) {
              setSelectedNodeId(model.nodes[0].id);
            }
            setActiveTab(val);
          }}
          aria-label="WorkItem workspace sections"
        >
          <ToggleButton value="workflow">Workflow</ToggleButton>
          <ToggleButton value="items">
            Items
            {realTracking
              ? trackingData && trackingData.trackedItems.length > 0
                ? ` (${trackingData.trackedItems.length})`
                : ''
              : model.nodes.length > 0
                ? ` (${model.nodes.length})`
                : ''}
          </ToggleButton>
          <ToggleButton value="history">History</ToggleButton>
        </ToggleButtonGroup>
      </Box>

      {/* 3. Tab Content Body */}
      {realTracking && tracking.notice && <Alert severity="warning">{tracking.notice}</Alert>}
      {realTracking && activeTab === 'workflow' && (
        <>
          <TrackingStateNotice view={trackingView} />
          {trackingData && <TrackingWorkflowPanel tracking={trackingData} now={now} />}
        </>
      )}
      {realTracking && activeTab === 'items' && (
        <>
          <TrackingStateNotice view={trackingView} />
          {trackingData && <TrackedItemsPanel tracking={trackingData} />}
        </>
      )}
      {!realTracking && activeTab === 'workflow' && (
        <WorkItemOverviewTab
          model={model}
          selectedNodeId={selectedNodeId}
          onSelectNode={setSelectedNodeId}
          onViewFullHistory={() => setActiveTab('history')}
          onViewItemDetails={(nodeId) => {
            const deepLink = createItemExplorerDeepLink(nodeId);
            setSelectedNodeId(deepLink.selectedItemId);
            setActiveTab(deepLink.activeTab);
          }}
        />
      )}
      {!realTracking && activeTab === 'items' && (
        <WorkItemItemsTab
          model={model}
          selectedNodeId={selectedNodeId}
          onSelectNode={setSelectedNodeId}
        />
      )}
      {realTracking && activeTab === 'history' && <WorkItemScopedHistory workItemId={item.id} />}
      {!realTracking && activeTab === 'history' && (
        <WorkItemHistoryTab
          item={item}
          model={model}
          selectedNodeId={selectedNodeId}
          onSelectNode={setSelectedNodeId}
        />
      )}

      <Dialog
        open={scheduleDialogOpen}
        onClose={() => !scheduleBusy && setScheduleDialogOpen(false)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>Schedule Action in Planner</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            {scheduleError && <Alert severity="error">{scheduleError}</Alert>}
            <TextField
              label="Action Title"
              size="small"
              fullWidth
              value={todoTitle}
              onChange={(e) => setTodoTitle(e.target.value)}
              disabled={scheduleBusy}
            />
            <Stack direction="row" spacing={1}>
              <TextField
                label="Due Date"
                type="date"
                size="small"
                fullWidth
                value={todoDueDate}
                onChange={(e) => setTodoDueDate(e.target.value)}
                slotProps={{ inputLabel: { shrink: true } }}
                disabled={scheduleBusy}
              />
              <TextField
                label="Due Time"
                type="time"
                size="small"
                fullWidth
                value={todoDueTime}
                onChange={(e) => setTodoDueTime(e.target.value)}
                slotProps={{ inputLabel: { shrink: true } }}
                disabled={scheduleBusy}
                helperText="Optional"
              />
            </Stack>
            {trackingData && trackingData.trackedItems.length > 0 && (
              <TextField
                select
                label="Tracked Item"
                size="small"
                fullWidth
                value={todoTrackedItemId}
                onChange={(e) => setTodoTrackedItemId(e.target.value)}
                disabled={scheduleBusy}
                helperText="Optional · links this action to Tracked Item health"
              >
                <MenuItem value="">No tracked item</MenuItem>
                {trackingData.trackedItems.map((trackedItem) => (
                  <MenuItem key={trackedItem.id} value={trackedItem.id}>
                    {trackedItem.title}
                  </MenuItem>
                ))}
              </TextField>
            )}
            <TextField
              select
              label="Reminder Policy"
              size="small"
              fullWidth
              value={todoReminderPolicy}
              onChange={(e) =>
                setTodoReminderPolicy(e.target.value as ScheduleReminderPolicy)
              }
              disabled={scheduleBusy}
            >
              <MenuItem value="standard">Standard (3d, 2d, 1d before)</MenuItem>
              <MenuItem value="urgent">Urgent / Same-day (2h, 30m before)</MenuItem>
              <MenuItem value="near">Near-term (15m before)</MenuItem>
              <MenuItem value="1d">1 day before</MenuItem>
              <MenuItem value="due">At due time</MenuItem>
              <MenuItem value="none">No reminder</MenuItem>
            </TextField>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setScheduleDialogOpen(false)} disabled={scheduleBusy}>
            Cancel
          </Button>
          <Button
            variant="contained"
            onClick={() => void handleCreateScheduleAction()}
            disabled={scheduleBusy || !todoTitle.trim()}
          >
            {scheduleBusy ? 'Scheduling…' : 'Schedule'}
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={Boolean(scheduleSuccess)}
        autoHideDuration={4000}
        onClose={() => setScheduleSuccess(null)}
        message={scheduleSuccess}
      />
    </Stack>
  );
}
