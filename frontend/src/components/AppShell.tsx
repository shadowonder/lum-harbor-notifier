import { useCallback, useEffect, useRef, useState } from 'react';
import { Box, Drawer, useMediaQuery } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import Header from './Header';
import Sidebar, { type PageId } from './Sidebar';
import AssistantPanel from './AssistantPanel';
import Dashboard from '../pages/Dashboard';
import History from '../pages/History';
import Notifications from '../pages/Notifications';
import Planner from '../pages/Planner';
import Settings from '../pages/Settings';
import WorkItems from '../pages/WorkItems';
import PreviewBanner from './proposals/PreviewBanner';
import type {
  ProductProposal,
  ProposalProductApi,
  ProposeWorkItemUpdateInput,
} from '../api/proposals/proposalProductApi.ts';
import { handoffCreatedProposal } from '../proposals/proposalHandoff.ts';
import { wailsProposalProductApi } from '../api/proposals/wailsProposalProductApi.ts';
import type { ProposalProjection } from '../presentation/proposals/buildPreviewState';
import type { PlannerNavigationIntent } from '../navigation/plannerNavigation';
import {
  createWorkItemNavigationIntent,
  type WorkItemNavigationIntent,
} from '../navigation/workItemNavigation';
import {
  saveGeneralSettings,
  saveInAppNotificationSettings,
  type GeneralSettings,
  type InAppNotificationSettings,
  type RuntimeConfig,
} from '../api/runtimeConfig';
import type { AssistantApi, AssistantSurface } from '../api/assistant/assistantApi.ts';
import { wailsAssistantApi } from '../api/assistant/wailsAssistantApi.ts';
import { sendAssistantMessage, type AssistantEntry } from '../assistant/assistantConversation.ts';
import NotificationToast from './notifications/NotificationToast.tsx';
import {
  proposalAppliedMessage,
  proposalRejectedMessage,
  proposalStaleMessage,
} from '../proposals/proposalActionMessages.ts';
import { useInAppNotificationAlerts } from '../notifications/useInAppNotificationAlerts.ts';
import { productNotificationsApi } from '../api/notifications/wailsNotificationsProductApi.ts';
import type { ProductNotificationsApi } from '../api/notifications/notificationsProductApi.ts';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { ConsumeNotificationActivations } from '../../wailsjs/go/app/App';
import {
  mapNotificationActivation,
  NOTIFICATION_ACTIVATION_EVENT,
} from '../notifications/notificationActivation.ts';

function pageToAssistantSurface(page: PageId): AssistantSurface {
  switch (page) {
    case 'workItems':
      return 'WORK_ITEMS';
    case 'planner':
      return 'PLANNER';
    case 'dashboard':
      return 'DASHBOARD';
    case 'notifications':
      return 'NOTIFICATIONS';
    case 'history':
      return 'HISTORY';
    case 'settings':
      return 'SETTINGS';
    default:
      return '';
  }
}
import {
  beginProposalReview,
  proposalForReviewSession,
  proposalTargetNavigation,
  type ProposalReviewSession,
} from '../proposals/proposalReviewSession.ts';

const pageTitles: Record<PageId, string> = {
  dashboard: 'Dashboard',
  planner: 'Planner',
  workItems: 'Work Items',
  notifications: 'Notifications',
  history: 'History',
  settings: 'Settings',
};

const SIDEBAR_STORAGE_KEY = 'lum-harbor-sidebar-collapsed';
// Background Source processing creates Proposals without any user action, so
// Needs Review re-reads the pending list on this cadence.
const PENDING_PROPOSAL_POLL_MS = 10_000;
const EMPTY_PROPOSAL_PROJECTION: ProposalProjection = {
  workItems: {},
  planner: { addedItems: [], enrichments: {} },
};

export interface ProposalActionNotice {
  severity: 'success' | 'info' | 'warning';
  message: string;
}

function getStoredSidebarState() {
  return localStorage.getItem(SIDEBAR_STORAGE_KEY) === 'true';
}

function PageWorkspace({
  page,
  generalSettings,
  onSaveGeneralSettings,
  dashboardTodoDisplayLimit,
  plannerNavigationIntent,
  workItemNavigationIntent,
  onAssistantContextChange,
  onSelectedWorkItemChange,
  onConsumePlannerNavigationIntent,
  onConsumeWorkItemNavigationIntent,
  onOpenWorkItems,
  onReviewSuggestions,
  onOpenPlanner,
  onOpenWorkItem,
  proposals,
  proposalsError,
  activeProposal,
  workItemRefreshGeneration,
  proposalRefreshTrigger,
  inAppNotificationSettings,
  onSaveInAppNotificationSettings,
  onTestNotification,
  onNotificationRead,
  onRetryProposals,
  onCreateProposal,
}: {
  page: PageId;
  generalSettings: GeneralSettings;
  onSaveGeneralSettings: (value: GeneralSettings) => Promise<void>;
  dashboardTodoDisplayLimit: number;
  plannerNavigationIntent: PlannerNavigationIntent | null;
  workItemNavigationIntent: WorkItemNavigationIntent | null;
  onAssistantContextChange: (context: string) => void;
  onSelectedWorkItemChange: (selection: { id: string; title: string } | null) => void;
  onConsumePlannerNavigationIntent: () => void;
  onConsumeWorkItemNavigationIntent: () => void;
  onOpenPlanner: (intent: PlannerNavigationIntent) => void;
  onOpenWorkItems: () => void;
  onReviewSuggestions: () => void;
  onOpenWorkItem: (id: string, title: string) => void;
  proposals: ProductProposal[];
  proposalsError: string | null;
  activeProposal: ProductProposal | null;
  workItemRefreshGeneration: number;
  proposalRefreshTrigger?: number;
  inAppNotificationSettings: InAppNotificationSettings;
  onSaveInAppNotificationSettings: (value: InAppNotificationSettings) => Promise<void>;
  onTestNotification: () => void;
  onNotificationRead: () => Promise<void>;
  onRetryProposals: () => void;
  onCreateProposal: (input: ProposeWorkItemUpdateInput) => Promise<ProductProposal>;
}) {
  switch (page) {
    case 'planner':
      return (
        <Planner
          onAssistantContextChange={onAssistantContextChange}
          navigationIntent={plannerNavigationIntent}
          onConsumeNavigationIntent={onConsumePlannerNavigationIntent}
          proposalState={EMPTY_PROPOSAL_PROJECTION}
          preview={null}
        />
      );
    case 'workItems':
      return (
        <WorkItems
          proposals={proposals}
          proposalsError={proposalsError}
          activeProposal={activeProposal}
          workItemRefreshGeneration={workItemRefreshGeneration}
          navigationIntent={workItemNavigationIntent}
          onConsumeNavigationIntent={onConsumeWorkItemNavigationIntent}
          onSelectedWorkItemChange={onSelectedWorkItemChange}
          onRetryProposals={onRetryProposals}
          onCreateProposal={onCreateProposal}
        />
      );
    case 'notifications':
      return (
        <Notifications
          onAssistantContextChange={onAssistantContextChange}
          onOpenWorkItem={onOpenWorkItem}
          onOpenPlanner={onOpenPlanner}
          onNotificationRead={onNotificationRead}
        />
      );
    case 'history':
      return <History onOpenWorkItem={onOpenWorkItem} />;
    case 'settings':
      return (
        <Settings
          generalSettings={generalSettings}
          onSaveGeneralSettings={onSaveGeneralSettings}
          inAppNotificationSettings={inAppNotificationSettings}
          onSaveInAppNotificationSettings={onSaveInAppNotificationSettings}
          onTestNotification={onTestNotification}
        />
      );
    case 'dashboard':
      return (
        <Dashboard
          todoDisplayLimit={dashboardTodoDisplayLimit}
          onOpenWorkItem={onOpenWorkItem}
          onOpenWorkItems={onOpenWorkItems}
          onReviewSuggestions={onReviewSuggestions}
          onOpenPlanner={onOpenPlanner}
          refreshTrigger={proposalRefreshTrigger}
        />
      );
  }
}

/** Hosts the application using the already loaded runtime configuration. */
export default function AppShell({
  runtimeConfig,
  assistantApi = wailsAssistantApi,
  proposalApi = wailsProposalProductApi,
  notificationsApi = productNotificationsApi,
}: {
  runtimeConfig: RuntimeConfig;
  assistantApi?: AssistantApi;
  proposalApi?: ProposalProductApi;
  notificationsApi?: ProductNotificationsApi;
}) {
  const theme = useTheme();
  const assistantDocked = useMediaQuery(theme.breakpoints.up('lg'));
  const [activePage, setActivePage] = useState<PageId>('dashboard');
  const { alerts, unreadCount, dismissAlert, presentTestNotification, refreshNotifications } =
    useInAppNotificationAlerts(notificationsApi);
  const [inAppNotificationSettings, setInAppNotificationSettings] =
    useState<InAppNotificationSettings>(runtimeConfig.notifications.inApp);
  const [generalSettings, setGeneralSettings] = useState<GeneralSettings>(
    runtimeConfig.general ?? { closeWindowBehavior: 'background' },
  );
  const [sidebarCollapsed, setSidebarCollapsed] = useState(getStoredSidebarState);
  const [assistantOpen, setAssistantOpen] = useState(false);
  const [assistantInput, setAssistantInput] = useState('');
  const [assistantContext, setAssistantContext] = useState('Dashboard');
  // Page-aware context: only the selected WorkItem's identity. WorkItem facts
  // are re-read by the backend for every request. The transcript is
  // application-lifetime UI state only and is never sent back as context.
  const [assistantWorkItemId, setAssistantWorkItemId] = useState<string | null>(null);
  const [assistantEntries, setAssistantEntries] = useState<AssistantEntry[]>([]);
  const [assistantSubmitting, setAssistantSubmitting] = useState(false);
  const activePageRef = useRef<PageId>('dashboard');
  const [plannerNavigationIntent, setPlannerNavigationIntent] =
    useState<PlannerNavigationIntent | null>(null);
  const [workItemNavigationIntent, setWorkItemNavigationIntent] =
    useState<WorkItemNavigationIntent | null>(null);
  const [proposals, setProposals] = useState<ProductProposal[]>([]);
  const [proposalsLoading, setProposalsLoading] = useState(true);
  const [proposalsError, setProposalsError] = useState<string | null>(null);
  const [proposalLoadGeneration, setProposalLoadGeneration] = useState(0);
  const [proposalReviewSession, setProposalReviewSession] = useState<ProposalReviewSession | null>(
    null,
  );
  const [proposalMutation, setProposalMutation] = useState<'apply' | 'reject' | null>(null);
  const [proposalActionError, setProposalActionError] = useState<string | null>(null);
  const [proposalActionNotice, setProposalActionNotice] = useState<ProposalActionNotice | null>(
    null,
  );
  const [workItemRefreshGeneration, setWorkItemRefreshGeneration] = useState(0);
  const assistantInputRef = useRef<HTMLInputElement>(null);
  const proposalMutationRef = useRef(false);
  const assistantOpenRef = useRef(false);
  const beginReviewRef = useRef<(proposal: ProductProposal) => void>(() => undefined);
  const saveGeneral = useCallback(async (value: GeneralSettings) => {
    const saved = await saveGeneralSettings(value);
    setGeneralSettings(saved);
  }, []);
  const saveNotificationSettings = useCallback(async (value: InAppNotificationSettings) => {
    const saved = await saveInAppNotificationSettings(value);
    setInAppNotificationSettings(saved);
  }, []);

  const markNotificationReadForInteraction = useCallback(
    async (notificationId: string) => {
      if (notificationId.startsWith('preview-')) return;
      try {
        await notificationsApi.markRead(notificationId);
      } catch (error) {
        console.warn('Could not mark notification read:', error);
      } finally {
        await refreshNotifications();
      }
    },
    [notificationsApi, refreshNotifications],
  );

  useEffect(() => {
    assistantOpenRef.current = assistantOpen;
  }, [assistantOpen]);

  useEffect(() => {
    localStorage.setItem(SIDEBAR_STORAGE_KEY, String(sidebarCollapsed));
  }, [sidebarCollapsed]);

  useEffect(() => {
    let active = true;
    void proposalApi.getPendingProposals().then(
      (items) => {
        if (!active) return;
        const pending = items.filter((proposal) => proposal.status === 'PENDING');
        setProposals(pending);
        setProposalReviewSession((session) =>
          session && !pending.some((proposal) => proposal.id === session.proposalId)
            ? null
            : session,
        );
        setProposalsError(null);
        setProposalsLoading(false);
      },
      (error: unknown) => {
        if (!active) return;
        setProposals([]);
        setProposalsError(error instanceof Error ? error.message : String(error));
        setProposalsLoading(false);
      },
    );
    return () => {
      active = false;
    };
  }, [proposalApi, proposalLoadGeneration]);

  useEffect(() => {
    const timer = setInterval(
      () => setProposalLoadGeneration((value) => value + 1),
      PENDING_PROPOSAL_POLL_MS,
    );
    return () => clearInterval(timer);
  }, []);

  const activeProposal = proposalForReviewSession(proposals, proposalReviewSession);
  const endProposalReview = () => {
    setProposalReviewSession(null);
    setProposalActionError(null);
  };
  const retryProposals = () => {
    setProposalsLoading(true);
    setProposalLoadGeneration((value) => value + 1);
  };
  const refreshPendingProposals = async () => {
    const items = await proposalApi.getPendingProposals();
    const pending = items.filter((proposal) => proposal.status === 'PENDING');
    setProposals(pending);
    setProposalsError(null);
    setProposalsLoading(false);
    setProposalReviewSession((session) =>
      session && !pending.some((proposal) => proposal.id === session.proposalId) ? null : session,
    );
    return pending;
  };
  const createProposal = async (input: ProposeWorkItemUpdateInput) => {
    const created = await proposalApi.proposeWorkItemUpdate(input);
    try {
      await refreshPendingProposals();
    } catch (error) {
      setProposalsError(error instanceof Error ? error.message : String(error));
    }
    return created;
  };
  const reconcileTerminalProposalAction = async (notice: ProposalActionNotice) => {
    setProposalReviewSession(null);
    setProposalActionError(null);
    setProposalActionNotice(notice);
    setWorkItemRefreshGeneration((value) => value + 1);
    try {
      await refreshPendingProposals();
    } catch (error) {
      setProposalsError(error instanceof Error ? error.message : String(error));
    }
  };
  const applyActiveProposal = async () => {
    if (!activeProposal || proposalMutationRef.current) return;
    proposalMutationRef.current = true;
    setProposalMutation('apply');
    setProposalActionError(null);
    setProposalActionNotice(null);
    try {
      const result = await proposalApi.applyProposal(activeProposal.id);
      const operationType = activeProposal.operationType;
      const actionCount = activeProposal.operations?.length ?? 0;
      const notice: ProposalActionNotice =
        result.outcome === 'APPLIED'
          ? { severity: 'success', message: proposalAppliedMessage(operationType, actionCount) }
          : result.outcome === 'STALE'
            ? { severity: 'warning', message: proposalStaleMessage(operationType, actionCount) }
            : {
                severity: 'info',
                message: 'This proposal was already applied. Authoritative state was refreshed.',
              };
      await reconcileTerminalProposalAction(notice);
    } catch (error) {
      setProposalActionError(error instanceof Error ? error.message : String(error));
    } finally {
      proposalMutationRef.current = false;
      setProposalMutation(null);
    }
  };
  const rejectActiveProposal = async () => {
    if (!activeProposal || proposalMutationRef.current) return;
    proposalMutationRef.current = true;
    setProposalMutation('reject');
    setProposalActionError(null);
    setProposalActionNotice(null);
    try {
      await proposalApi.rejectProposal(activeProposal.id);
      await reconcileTerminalProposalAction({
        severity: 'info',
        message: proposalRejectedMessage(
          activeProposal.operationType,
          activeProposal.operations?.length ?? 0,
        ),
      });
    } catch (error) {
      setProposalActionError(error instanceof Error ? error.message : String(error));
    } finally {
      proposalMutationRef.current = false;
      setProposalMutation(null);
    }
  };
  /**
   * Foreground handoff for an AI-created Proposal. The Assistant result only
   * supplies the identity: review starts from the refreshed authoritative
   * collection through the same beginReview used by Needs Review cards.
   */
  const handoffToReview = (proposalId: string) =>
    handoffCreatedProposal(
      {
        refreshPending: refreshPendingProposals,
        beginReview: (proposal) => beginReviewRef.current(proposal),
        // Respect a user who closed the Assistant while the request was in flight.
        isAssistantOpen: () => assistantOpenRef.current,
      },
      proposalId,
    );
  const sendAssistant = async (message: string) => {
    setAssistantSubmitting(true);
    const produced = await sendAssistantMessage(
      assistantApi,
      message,
      assistantWorkItemId,
      handoffToReview,
      pageToAssistantSurface(activePageRef.current),
    );
    setAssistantEntries((entries) => [...entries, ...produced]);
    setAssistantSubmitting(false);
  };
  const selectWorkItemForAssistant = useCallback(
    (selection: { id: string; title: string } | null) => {
      setAssistantWorkItemId(selection?.id ?? null);
      if (selection) setAssistantContext(selection.title);
      else if (activePageRef.current === 'workItems') setAssistantContext('Work Items');
    },
    [],
  );
  const openAndFocusAssistant = useCallback(() => {
    setAssistantOpen(true);
    requestAnimationFrame(() => assistantInputRef.current?.focus());
  }, []);

  const closeAssistant = useCallback(() => {
    setAssistantOpen(false);
    setProposalReviewSession(null);
    setProposalActionError(null);
  }, []);

  const reviewSuggestions = useCallback(() => {
    setProposalReviewSession(null);
    setProposalActionError(null);
    setAssistantOpen(true);
    requestAnimationFrame(() => assistantInputRef.current?.focus());
  }, []);

  const beginReview = (proposal: ProductProposal) => {
    const targetNavigation = proposalTargetNavigation(
      activePageRef.current,
      assistantWorkItemId,
      proposal,
    );
    setProposalActionError(null);
    setProposalActionNotice(null);
    setProposalReviewSession(beginProposalReview(proposal));
    if (targetNavigation !== 'NONE' && proposal.targetWorkItemId !== null) {
      setWorkItemNavigationIntent(createWorkItemNavigationIntent(proposal.targetWorkItemId));
      activePageRef.current = 'workItems';
      setActivePage('workItems');
      setAssistantContext('Work Items');
    }
  };

  useEffect(() => {
    beginReviewRef.current = beginReview;
  });

  const navigate = (nextPage: PageId) => {
    if (nextPage === 'planner') setPlannerNavigationIntent(null);
    if (nextPage !== 'workItems') {
      setAssistantWorkItemId(null);
    }
    if (nextPage === 'workItems') {
      setWorkItemNavigationIntent(
        activeProposal &&
          activeProposal.targetWorkItemId !== null &&
          assistantWorkItemId !== activeProposal.targetWorkItemId
          ? createWorkItemNavigationIntent(activeProposal.targetWorkItemId)
          : null,
      );
    }
    activePageRef.current = nextPage;
    setActivePage(nextPage);
    setAssistantContext(pageTitles[nextPage] ?? '');
  };

  const navigateRef = useRef(navigate);
  useEffect(() => {
    navigateRef.current = navigate;
  });

  /**
   * Transfers a one-shot Dashboard intent to a newly mounted Planner. Planner
   * consumes the intent after capturing identity/date/rail state locally.
   */
  const openPlanner = (intent: PlannerNavigationIntent) => {
    setPlannerNavigationIntent(intent);
    activePageRef.current = 'planner';
    setActivePage('planner');
    setAssistantContext('Planner');
  };

  const openWorkItem = (id: string, title: string) => {
    setWorkItemNavigationIntent(createWorkItemNavigationIntent(id));
    activePageRef.current = 'workItems';
    setActivePage('workItems');
    setAssistantContext(title);
  };

  const openWorkItems = () => {
    setWorkItemNavigationIntent(
      activeProposal && activeProposal.targetWorkItemId !== null
        ? createWorkItemNavigationIntent(activeProposal.targetWorkItemId)
        : null,
    );
    activePageRef.current = 'workItems';
    setActivePage('workItems');
    setAssistantContext('Work Items');
  };

  const handleNativeNotificationActivation = useCallback(
    (raw: unknown) => {
      const activation = mapNotificationActivation(raw);
      if (!activation) return;

      void markNotificationReadForInteraction(activation.notificationId);
      if (activation.plannerItemId) {
        setPlannerNavigationIntent({
          selectedPlannerItemId: activation.plannerItemId,
          preferredRailMode: 'open',
        });
        activePageRef.current = 'planner';
        setActivePage('planner');
        setAssistantContext('Planner');
      } else if (activation.workItemId) {
        setWorkItemNavigationIntent(createWorkItemNavigationIntent(activation.workItemId));
        activePageRef.current = 'workItems';
        setActivePage('workItems');
        setAssistantContext('Work Item');
      } else {
        activePageRef.current = 'notifications';
        setActivePage('notifications');
        setAssistantContext('Notifications');
      }
    },
    [markNotificationReadForInteraction],
  );

  useEffect(() => {
    const unsubscribe = EventsOn(NOTIFICATION_ACTIVATION_EVENT, (value: unknown) =>
      handleNativeNotificationActivation(value),
    );
    void ConsumeNotificationActivations().then(
      (items) => items.forEach(handleNativeNotificationActivation),
      (error: unknown) => console.warn('Could not load pending notification activations:', error),
    );
    return unsubscribe;
  }, [handleNativeNotificationActivation]);

  useEffect(() => {
    const unsubscribe = EventsOn('lumharbor:navigate-page', (target: unknown) => {
      if (target === 'settings') {
        navigateRef.current('settings');
      }
    });
    return unsubscribe;
  }, []);

  useEffect(() => {
    if (assistantOpen) requestAnimationFrame(() => assistantInputRef.current?.focus());
  }, [assistantOpen, assistantDocked]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        openAndFocusAssistant();
      } else if (event.key === 'Escape' && assistantOpen) {
        closeAssistant();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [assistantOpen, closeAssistant, openAndFocusAssistant]);

  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: {
          xs: sidebarCollapsed ? '68px minmax(0, 1fr)' : '176px minmax(0, 1fr)',
          md: sidebarCollapsed ? '68px minmax(0, 1fr)' : '208px minmax(0, 1fr)',
          lg:
            assistantOpen && assistantDocked
              ? `${sidebarCollapsed ? 68 : 208}px minmax(0, 1fr) 400px`
              : `${sidebarCollapsed ? 68 : 208}px minmax(0, 1fr)`,
        },
        gridTemplateRows: '60px minmax(0, 1fr)',
        width: '100%',
        height: '100%',
        minWidth: 0,
        transition: 'grid-template-columns 180ms ease',
      }}
    >
      <Header
        onOpenAssistant={openAndFocusAssistant}
        onOpenNotifications={() => navigate('notifications')}
        unreadCount={unreadCount}
      />
      <Sidebar
        activePage={activePage}
        collapsed={sidebarCollapsed}
        onNavigate={navigate}
        onToggleCollapsed={() => setSidebarCollapsed((collapsed) => !collapsed)}
      />
      <Box
        component="main"
        sx={{
          minWidth: 0,
          minHeight: 0,
          overflow: 'auto',
          scrollbarGutter: 'stable',
          containerType: 'inline-size',
        }}
      >
        {activeProposal && <PreviewBanner proposal={activeProposal} onExit={endProposalReview} />}
        <PageWorkspace
          page={activePage}
          generalSettings={generalSettings}
          onSaveGeneralSettings={saveGeneral}
          dashboardTodoDisplayLimit={runtimeConfig.dashboard.todoDisplayLimit}
          plannerNavigationIntent={plannerNavigationIntent}
          workItemNavigationIntent={workItemNavigationIntent}
          onAssistantContextChange={setAssistantContext}
          onSelectedWorkItemChange={selectWorkItemForAssistant}
          onConsumePlannerNavigationIntent={() => setPlannerNavigationIntent(null)}
          onConsumeWorkItemNavigationIntent={() => setWorkItemNavigationIntent(null)}
          onOpenPlanner={openPlanner}
          onOpenWorkItems={openWorkItems}
          onReviewSuggestions={reviewSuggestions}
          onOpenWorkItem={openWorkItem}
          proposals={proposals}
          proposalsError={proposalsError}
          activeProposal={activeProposal}
          workItemRefreshGeneration={workItemRefreshGeneration}
          proposalRefreshTrigger={proposalLoadGeneration}
          inAppNotificationSettings={inAppNotificationSettings}
          onSaveInAppNotificationSettings={saveNotificationSettings}
          onTestNotification={presentTestNotification}
          onNotificationRead={refreshNotifications}
          onRetryProposals={retryProposals}
          onCreateProposal={createProposal}
        />
      </Box>

      {assistantOpen && assistantDocked && (
        <Box
          sx={{
            gridColumn: 3,
            gridRow: 2,
            minWidth: 0,
            minHeight: 0,
            borderLeft: 1,
            borderColor: 'divider',
          }}
        >
          <AssistantPanel
            context={assistantContext}
            input={assistantInput}
            inputRef={assistantInputRef}
            onInputChange={setAssistantInput}
            onClose={closeAssistant}
            proposals={proposals}
            proposalsLoading={proposalsLoading}
            proposalsError={proposalsError}
            activeProposal={activeProposal}
            proposalMutation={proposalMutation}
            proposalActionError={proposalActionError}
            proposalActionNotice={proposalActionNotice}
            onBeginReview={beginReview}
            onEndReview={endProposalReview}
            onApplyProposal={applyActiveProposal}
            onRejectProposal={rejectActiveProposal}
            onRetryProposals={retryProposals}
            entries={assistantEntries}
            submitting={assistantSubmitting}
            onSend={sendAssistant}
          />
        </Box>
      )}

      {!assistantDocked && (
        <Drawer
          anchor="right"
          open={assistantOpen}
          onClose={closeAssistant}
          slotProps={{ paper: { sx: { width: 400, maxWidth: '88vw' } } }}
        >
          <AssistantPanel
            context={assistantContext}
            input={assistantInput}
            inputRef={assistantInputRef}
            onInputChange={setAssistantInput}
            onClose={closeAssistant}
            proposals={proposals}
            proposalsLoading={proposalsLoading}
            proposalsError={proposalsError}
            activeProposal={activeProposal}
            proposalMutation={proposalMutation}
            proposalActionError={proposalActionError}
            proposalActionNotice={proposalActionNotice}
            onBeginReview={beginReview}
            onEndReview={endProposalReview}
            onApplyProposal={applyActiveProposal}
            onRejectProposal={rejectActiveProposal}
            onRetryProposals={retryProposals}
            entries={assistantEntries}
            submitting={assistantSubmitting}
            onSend={sendAssistant}
          />
        </Drawer>
      )}

      <NotificationToast
        items={alerts}
        settings={inAppNotificationSettings}
        onClose={dismissAlert}
        onOpenTodo={(plannerItemId) =>
          openPlanner({ selectedPlannerItemId: plannerItemId, preferredRailMode: 'open' })
        }
        onOpenWorkItem={openWorkItem}
        onOpenNotifications={() => navigate('notifications')}
        onInteract={(notificationId) => void markNotificationReadForInteraction(notificationId)}
      />
    </Box>
  );
}
