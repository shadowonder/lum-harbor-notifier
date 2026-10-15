import { useEffect, useReducer, useState } from 'react';
import AddRounded from '@mui/icons-material/AddRounded';
import { Alert, Box, Button, Paper, Stack, Typography } from '@mui/material';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';
import {
  createProductWorkItem,
  type ProductWorkItem,
  type ProductWorkItemApi,
  type ProductWorkItemLifecycle,
  type ProductWorkItemSummary,
  setProductWorkItemLifecycle,
} from '../api/workItems/productApi.ts';
import { productWorkItemApi } from '../api/workItems/wailsProductWorkItemApi.ts';
import type {
  ProductProposal,
  ProposeWorkItemUpdateInput,
} from '../api/proposals/proposalProductApi.ts';
import {
  consumeWorkItemNavigationIntent,
  type WorkItemNavigationIntent,
} from '../navigation/workItemNavigation.ts';
import {
  initialWorkItemsPageState,
  groupWorkItemsByLifecycle,
  selectedWorkItem,
  workItemsPageReducer,
} from './workItemsPageState.ts';
import WorkItemRow from '../components/workItems/WorkItemRow.tsx';
import WorkItemCreateDialog from '../components/workItems/WorkItemCreateDialog.tsx';
import WorkItemSuggestChangeDialog, {
  type SuggestChangeFormValues,
} from '../components/workItems/WorkItemSuggestChangeDialog.tsx';
import WorkItemWorkspace from '../components/workItems/workspace/WorkItemWorkspace.tsx';

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function WorkItemListSection({
  label,
  items,
  secondary = false,
  onOpenWorkspace,
  summaries,
  proposalCounts,
}: {
  label: string;
  items: ProductWorkItem[];
  secondary?: boolean;
  onOpenWorkspace: (id: string) => void;
  summaries?: Record<string, ProductWorkItemSummary>;
  proposalCounts?: Map<string, number>;
}) {
  if (items.length === 0) return null;
  return (
    <Box component="section" sx={secondary ? { opacity: 0.84 } : undefined}>
      <Stack
        direction="row"
        sx={{ mb: 1, alignItems: 'baseline', justifyContent: 'space-between' }}
      >
        <Typography
          component="h2"
          variant="h2"
          sx={{ color: secondary ? 'text.secondary' : 'text.primary' }}
        >
          {label} Work Items
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {items.length} {items.length === 1 ? 'item' : 'items'}
        </Typography>
      </Stack>
      <Stack component="ul" spacing={1} sx={{ p: 0, m: 0, listStyle: 'none' }}>
        {items.map((item) => (
          <WorkItemRow
            key={item.id}
            item={item}
            summary={summaries?.[item.id]}
            onOpenWorkspace={onOpenWorkspace}
            pendingProposalCount={proposalCounts?.get(item.id) ?? 0}
          />
        ))}
      </Stack>
    </Box>
  );
}

export default function WorkItems({
  api = productWorkItemApi,
  proposals = [],
  proposalsError = null,
  activeProposal = null,
  workItemRefreshGeneration = 0,
  navigationIntent = null,
  onConsumeNavigationIntent,
  onSelectedWorkItemChange,
  onRetryProposals,
  onCreateProposal,
}: {
  api?: ProductWorkItemApi;
  proposals?: ProductProposal[];
  proposalsError?: string | null;
  activeProposal?: ProductProposal | null;
  workItemRefreshGeneration?: number;
  navigationIntent?: WorkItemNavigationIntent | null;
  onConsumeNavigationIntent?: () => void;
  /** Publishes only the selected WorkItem's identity to the global Assistant. */
  onSelectedWorkItemChange?: (selection: { id: string; title: string } | null) => void;
  onRetryProposals?: () => void;
  onCreateProposal?: (input: ProposeWorkItemUpdateInput) => Promise<ProductProposal>;
}) {
  const [state, dispatch] = useReducer(workItemsPageReducer, initialWorkItemsPageState);
  const [loadGeneration, setLoadGeneration] = useState(0);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);

  const [suggestDialogOpen, setSuggestDialogOpen] = useState(false);
  const [suggestSubmitting, setSuggestSubmitting] = useState(false);
  const [suggestError, setSuggestError] = useState<string | null>(null);

  const selected = selectedWorkItem(state);
  const lifecycleGroups = groupWorkItemsByLifecycle(state.items);
  const proposalTestToolEnabled = import.meta.env.DEV && Boolean(onCreateProposal);

  const selectedId = selected?.id ?? null;
  const selectedTitle = selected?.title ?? '';
  useEffect(() => {
    onSelectedWorkItemChange?.(selectedId ? { id: selectedId, title: selectedTitle } : null);
  }, [onSelectedWorkItemChange, selectedId, selectedTitle]);
  useEffect(() => () => onSelectedWorkItemChange?.(null), [onSelectedWorkItemChange]);

  // WorkItem truth is local to this product page; Proposal truth is supplied by AppShell.
  useEffect(() => {
    let active = true;
    dispatch({ type: 'loadStarted' });
    void api.overview().then(
      (overview) => {
        if (!active) return;
        dispatch({
          type: 'loadSucceeded',
          items: overview.items.map((entry) => entry.workItem),
          summaries: Object.fromEntries(
            overview.items.map((entry) => [
              entry.workItem.id,
              { tracking: entry.tracking, attention: entry.attention },
            ]),
          ),
        });
      },
      (error: unknown) => active && dispatch({ type: 'loadFailed', message: errorMessage(error) }),
    );
    return () => {
      active = false;
    };
  }, [api, loadGeneration, workItemRefreshGeneration]);

  useEffect(() => {
    if (!navigationIntent || state.status !== 'ready') return;
    consumeWorkItemNavigationIntent(
      navigationIntent,
      state.items,
      (id) => dispatch({ type: 'selected', id }),
      () => onConsumeNavigationIntent?.(),
    );
  }, [navigationIntent, onConsumeNavigationIntent, state.items, state.status]);

  const handleOpenWorkspace = (id: string) => {
    dispatch({ type: 'selected', id });
  };

  const handleBackToOverview = () => {
    if (!state.lifecycleSaving) {
      dispatch({ type: 'closed' });
    }
  };

  const handleCreateSubmit = async (title: string, description: string) => {
    dispatch({ type: 'createStarted' });
    try {
      const item = await createProductWorkItem(api, { title, description });
      dispatch({ type: 'createSucceeded', item });
      setCreateDialogOpen(false);
    } catch (error) {
      dispatch({ type: 'createFailed', message: errorMessage(error) });
    }
  };

  const handleChangeLifecycle = async (targetLifecycleState: ProductWorkItemLifecycle) => {
    if (!selected || state.lifecycleSaving) return;
    dispatch({ type: 'lifecycleStarted' });
    try {
      const item = await setProductWorkItemLifecycle(api, {
        id: selected.id,
        targetLifecycleState,
      });
      dispatch({ type: 'lifecycleSucceeded', item });
    } catch (error) {
      dispatch({ type: 'lifecycleFailed', message: errorMessage(error) });
    }
  };

  // Proposal handlers
  const handleOpenSuggestChange = () => {
    setSuggestError(null);
    setSuggestDialogOpen(true);
  };

  const handleSuggestSubmit = async (values: SuggestChangeFormValues) => {
    if (!selected || !onCreateProposal) return;
    setSuggestSubmitting(true);
    setSuggestError(null);
    try {
      await onCreateProposal({
        targetWorkItemId: selected.id,
        proposedTitle: values.proposedTitle,
        proposedDescription: values.proposedDescription,
        reason: values.reason,
      });
      setSuggestDialogOpen(false);
    } catch (error) {
      setSuggestError(errorMessage(error));
    } finally {
      setSuggestSubmitting(false);
    }
  };

  // Build proposal count map by targetWorkItemId. A root-create Proposal
  // (targetWorkItemId === null) belongs to no Work Item row and is skipped.
  const proposalCounts = new Map<string, number>();
  for (const p of proposals) {
    if (p.targetWorkItemId === null) continue;
    proposalCounts.set(p.targetWorkItemId, (proposalCounts.get(p.targetWorkItemId) ?? 0) + 1);
  }

  // If a WorkItem is selected, enter the full WorkItem workspace!
  if (selected) {
    return (
      <PageContainer>
        <WorkItemWorkspace
          key={`${selected.id}:${workItemRefreshGeneration}`}
          item={selected}
          state={state}
          onBack={handleBackToOverview}
          onChangeLifecycle={handleChangeLifecycle}
          activeProposal={activeProposal}
          onOpenProposalTestTool={proposalTestToolEnabled ? handleOpenSuggestChange : undefined}
        />

        {proposalTestToolEnabled && (
          <WorkItemSuggestChangeDialog
            open={suggestDialogOpen}
            onClose={() => setSuggestDialogOpen(false)}
            targetWorkItem={selected}
            onSubmit={handleSuggestSubmit}
            submitting={suggestSubmitting}
            error={suggestError}
          />
        )}
      </PageContainer>
    );
  }

  // Otherwise, render the Work Items Overview
  return (
    <PageContainer>
      <PageHeader
        eyebrow="Long-term outcomes"
        title="Work Items"
        subtitle="Manage outcomes, baseline trunk progressions, and branch workflows over time."
        status={state.status === 'ready' ? `${state.items.length} work items` : undefined}
        action={
          <Button
            variant="contained"
            startIcon={<AddRounded />}
            onClick={() => setCreateDialogOpen(true)}
          >
            Create Work Item
          </Button>
        }
      />

      {state.status === 'loading' && (
        <Paper variant="outlined" sx={{ p: 4 }}>
          <Typography color="text.secondary">Loading Work Items…</Typography>
        </Paper>
      )}

      {state.status === 'error' && (
        <Alert
          severity="error"
          action={<Button onClick={() => setLoadGeneration((value) => value + 1)}>Retry</Button>}
        >
          Work Items could not be loaded: {state.readError}
        </Alert>
      )}

      {proposalsError && (
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={
            onRetryProposals ? (
              <Button color="inherit" size="small" onClick={onRetryProposals}>
                Retry
              </Button>
            ) : undefined
          }
        >
          Could not load suggestions: {proposalsError}. Work Items remain fully functional.
        </Alert>
      )}

      {state.status === 'ready' && state.items.length === 0 && (
        <Paper variant="outlined" sx={{ p: 5, textAlign: 'center' }}>
          <Typography variant="h3" sx={{ fontWeight: 600 }}>
            No Work Items yet
          </Typography>
          <Typography color="text.secondary" sx={{ mt: 1, maxWidth: 440, mx: 'auto' }}>
            Create your first feature outcome or long-term objective to establish a delivery trunk.
          </Typography>
          <Button
            variant="contained"
            startIcon={<AddRounded />}
            onClick={() => setCreateDialogOpen(true)}
            sx={{ mt: 2.5 }}
          >
            Create Work Item
          </Button>
        </Paper>
      )}

      {state.status === 'ready' && state.items.length > 0 && (
        <Stack spacing={3}>
          <WorkItemListSection
            label="Current"
            items={lifecycleGroups.current}
            onOpenWorkspace={handleOpenWorkspace}
            summaries={state.summaries}
            proposalCounts={proposalCounts}
          />
          <WorkItemListSection
            label="Closed"
            items={lifecycleGroups.closed}
            secondary
            onOpenWorkspace={handleOpenWorkspace}
            summaries={state.summaries}
            proposalCounts={proposalCounts}
          />
        </Stack>
      )}

      <WorkItemCreateDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
        onCreate={handleCreateSubmit}
        submitting={state.submitting}
        error={state.createError}
      />
    </PageContainer>
  );
}
