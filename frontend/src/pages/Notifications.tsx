import NotificationsNoneRounded from '@mui/icons-material/NotificationsNoneRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import {
  Alert,
  Box,
  Button,
  InputAdornment,
  Menu,
  MenuItem,
  Paper,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useState, type MouseEvent } from 'react';
import NotificationRow from '../components/notifications/NotificationRow';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';
import type { PlannerNavigationIntent } from '../navigation/plannerNavigation';
import type {
  CenterNotification,
  ProductNotificationsApi,
} from '../api/notifications/notificationsProductApi.ts';
import { productNotificationsApi } from '../api/notifications/wailsNotificationsProductApi.ts';
import {
  countByFilter,
  FILTER_LABELS,
  matchesFilter,
  matchesSearch,
  snoozeOptions,
  splitByDay,
  type NotificationFilter,
} from '../notifications/notificationCenterModel.ts';
import { useNotificationCenter } from '../notifications/useNotificationCenter.ts';

interface NotificationsProps {
  onAssistantContextChange?: (context: string) => void;
  /** Opens the owning WorkItem workspace by its real ID (existing AppShell navigation). */
  onOpenWorkItem?: (id: string, title: string) => void;
  /** Opens Planner with the target Todo selected. */
  onOpenPlanner?: (intent: PlannerNavigationIntent) => void;
  onNotificationRead?: () => Promise<void>;
  api?: ProductNotificationsApi;
}

export default function Notifications({
  onAssistantContextChange,
  onOpenWorkItem,
  onOpenPlanner,
  onNotificationRead,
  api = productNotificationsApi,
}: NotificationsProps) {
  const center = useNotificationCenter(api);
  const [filter, setFilter] = useState<NotificationFilter>('all');
  const [search, setSearch] = useState('');
  const [expandedNotificationId, setExpandedNotificationId] = useState<string | null>(null);
  const [snoozeAnchor, setSnoozeAnchor] = useState<HTMLElement | null>(null);
  const [snoozeIssueId, setSnoozeIssueId] = useState<string | null>(null);

  const items: CenterNotification[] = center.view.status === 'ready' ? center.view.items : [];
  const counts = countByFilter(items);
  const visibleItems = items.filter(
    (item) => matchesFilter(item, filter) && matchesSearch(item, search),
  );
  const now = new Date();
  const { today: todayItems, earlier: earlierItems } = splitByDay(visibleItems, now);

  const restoreNotificationsContext = () => onAssistantContextChange?.('Notifications');

  const changeFilter = (_event: MouseEvent<HTMLElement>, nextFilter: NotificationFilter | null) => {
    if (!nextFilter) return;
    setFilter(nextFilter);
    setExpandedNotificationId(null);
    restoreNotificationsContext();
  };

  const changeSearch = (value: string) => {
    setSearch(value);
    setExpandedNotificationId(null);
    restoreNotificationsContext();
  };

  const toggleNotification = (item: CenterNotification) => {
    void markRead(item);
    const nextExpandedId =
      expandedNotificationId === item.notificationId ? null : item.notificationId;
    setExpandedNotificationId(nextExpandedId);
    onAssistantContextChange?.(nextExpandedId ? `Notification: ${item.title}` : 'Notifications');
  };

  const markRead = async (item: CenterNotification) => {
    if (item.readAt !== null) return;
    const outcome = await center.run(() => api.markRead(item.notificationId));
    if (outcome.status !== 'mutationFailed') await onNotificationRead?.();
  };

  const openItem = (item: CenterNotification) => {
    void markRead(item);
    if (item.plannerItemId) {
      onOpenPlanner?.({
        selectedPlannerItemId: item.plannerItemId,
        preferredRailMode: 'open',
      });
      return;
    }
    if (item.workItemId) {
      onOpenWorkItem?.(item.workItemId, item.workItemTitle ?? 'Work Item');
    }
  };

  const openSnoozeMenu = (issueId: string, anchor: HTMLElement) => {
    setSnoozeIssueId(issueId);
    setSnoozeAnchor(anchor);
  };

  const closeSnoozeMenu = () => {
    setSnoozeAnchor(null);
    setSnoozeIssueId(null);
  };

  const applySnooze = (until: Date) => {
    if (!snoozeIssueId) return;
    const issueId = snoozeIssueId;
    closeSnoozeMenu();
    setExpandedNotificationId(null);
    void center.run(() => api.snooze(issueId, until));
  };

  const renderSection = (label: string, sectionItems: CenterNotification[]) => {
    if (sectionItems.length === 0) return null;
    return (
      <Box component="section" aria-labelledby={`notification-section-${label.toLowerCase()}`}>
        <Box
          sx={{
            px: 'clamp(14px, 1.8cqw, 22px)',
            py: 0.85,
            borderBottom: 1,
            borderColor: 'divider',
            bgcolor: 'surface.subtle',
          }}
        >
          <Typography
            id={`notification-section-${label.toLowerCase()}`}
            variant="caption"
            color="text.secondary"
            sx={{ fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase' }}
          >
            {label}
          </Typography>
        </Box>
        {sectionItems.map((item) => (
          <NotificationRow
            key={item.notificationId}
            item={item}
            now={now}
            busy={center.mutating}
            expanded={expandedNotificationId === item.notificationId}
            onToggle={() => toggleNotification(item)}
            onOpenItem={() => openItem(item)}
            onOpenSnooze={(anchor) => openSnoozeMenu(item.attentionIssueId, anchor)}
          />
        ))}
      </Box>
    );
  };

  return (
    <PageContainer>
      <PageHeader
        eyebrow="Stay informed"
        title="Notifications"
        subtitle="Review alerts and items that needed your attention."
        status={
          center.view.status === 'ready'
            ? `${counts.unread} unread · ${counts.needsAction} need action · ${counts.snoozed} snoozed`
            : undefined
        }
      />

      <Paper
        variant="outlined"
        sx={{
          display: 'flex',
          minWidth: 0,
          minHeight: 50,
          mb: 1.5,
          px: 1,
          py: 0.75,
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 1.5,
          flexWrap: 'wrap',
          bgcolor: 'surface.subtle',
        }}
      >
        <ToggleButtonGroup
          exclusive
          size="small"
          value={filter}
          onChange={changeFilter}
          aria-label="Filter notifications"
          sx={{
            maxWidth: '100%',
            '& .MuiToggleButton-root': {
              minHeight: 30,
              px: 'clamp(8px, 1.2cqw, 14px)',
              py: 0.35,
              borderColor: 'divider',
              color: 'text.secondary',
              fontSize: 11,
              fontWeight: 650,
              textTransform: 'none',
              '&.Mui-selected': {
                bgcolor: 'primary.light',
                color: 'primary.dark',
                boxShadow: 'inset 0 0 0 1px currentColor',
              },
              '&.Mui-selected:hover': { bgcolor: 'primary.light' },
              '&:focus-visible': {
                outline: '2px solid',
                outlineColor: 'primary.main',
                outlineOffset: 1,
              },
            },
          }}
        >
          {(Object.keys(FILTER_LABELS) as NotificationFilter[]).map((filterId) => (
            <ToggleButton key={filterId} value={filterId}>
              {FILTER_LABELS[filterId]}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>

        <TextField
          value={search}
          onChange={(event) => changeSearch(event.target.value)}
          size="small"
          placeholder="Search notifications"
          aria-label="Search notifications"
          sx={{ width: 'clamp(190px, 25cqw, 280px)', maxWidth: '100%' }}
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchRounded sx={{ fontSize: 17, color: 'icon.muted' }} />
                </InputAdornment>
              ),
              sx: { minHeight: 32, bgcolor: 'background.paper', fontSize: 12 },
            },
          }}
        />
      </Paper>

      {(center.notice || center.commandError) && (
        <Alert severity={center.commandError ? 'error' : 'warning'} sx={{ mb: 1.5 }}>
          {center.commandError ?? center.notice}
        </Alert>
      )}
      {center.view.status === 'error' && (
        <Alert
          severity="error"
          sx={{ mb: 1.5 }}
          action={
            <Button color="inherit" size="small" onClick={() => void center.reload()}>
              Retry
            </Button>
          }
        >
          Notifications could not be loaded: {center.view.message}
        </Alert>
      )}

      <Paper variant="outlined" sx={{ minWidth: 0, overflow: 'hidden' }}>
        {center.view.status === 'loading' ? (
          <Box sx={{ p: 3 }}>
            <Typography color="text.secondary">Loading notifications…</Typography>
          </Box>
        ) : center.view.status === 'error' ? null : visibleItems.length > 0 ? (
          <>
            {renderSection('Today', todayItems)}
            {renderSection('Earlier', earlierItems)}
          </>
        ) : (
          <Box
            sx={{
              display: 'grid',
              minHeight: 260,
              placeItems: 'center',
              p: 3,
              textAlign: 'center',
            }}
          >
            <Box sx={{ maxWidth: 360 }}>
              <Box
                sx={{
                  display: 'grid',
                  placeItems: 'center',
                  width: 38,
                  height: 38,
                  mx: 'auto',
                  mb: 1.25,
                  borderRadius: 1,
                  bgcolor: 'surface.secondary',
                  color: 'icon.muted',
                }}
              >
                <NotificationsNoneRounded sx={{ fontSize: 20 }} />
              </Box>
              <Typography sx={{ fontSize: 14, fontWeight: 650 }}>You’re all caught up.</Typography>
              <Typography color="text.secondary" sx={{ mt: 0.5 }}>
                {items.length === 0
                  ? 'No notifications yet.'
                  : 'No notifications match the current filter.'}
              </Typography>
            </Box>
          </Box>
        )}
      </Paper>

      <Menu
        anchorEl={snoozeAnchor}
        open={Boolean(snoozeAnchor)}
        onClose={closeSnoozeMenu}
        slotProps={{ paper: { sx: { minWidth: 190 } } }}
      >
        {snoozeOptions(now).map((option) => (
          <MenuItem key={option.label} dense onClick={() => applySnooze(option.until)}>
            {option.label}
          </MenuItem>
        ))}
      </Menu>
    </PageContainer>
  );
}
