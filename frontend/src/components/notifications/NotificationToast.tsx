import { type MouseEvent, useCallback, useEffect, useRef, useState } from 'react';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import EventNoteRounded from '@mui/icons-material/EventNoteRounded';
import UnfoldLessRounded from '@mui/icons-material/UnfoldLessRounded';
import UnfoldMoreRounded from '@mui/icons-material/UnfoldMoreRounded';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Divider,
  IconButton,
  Portal,
  Stack,
  Typography,
} from '@mui/material';
import type { CenterNotification } from '../../api/notifications/notificationsProductApi.ts';
import type { InAppNotificationSettings, NotificationPlacement } from '../../api/runtimeConfig.ts';
import {
  COMPACT_DECK_THRESHOLD,
  createNotificationLifetime,
  notificationLifetimeRemainingMs,
  resolveNotificationDeck,
  type NotificationCardMode,
  type NotificationLifetime,
} from './notificationDeck.ts';

export interface NotificationToastProps {
  items: CenterNotification[];
  settings: InAppNotificationSettings;
  onClose: (notificationId: string) => void;
  onOpenTodo?: (plannerItemId: string) => void;
  onOpenWorkItem?: (workItemId: string, title: string) => void;
  onOpenNotifications?: () => void;
  onInteract?: (notificationId: string) => void;
}

const ENTER_ANIMATION_MS = 180;
const EXIT_ANIMATION_MS = 140;
const DECK_ANIMATION_MS = 180;
const NOTIFICATION_LIST_ID = 'in-app-notification-list';

function placementStyle(placement: NotificationPlacement) {
  const top = placement.startsWith('TOP');
  const left = placement.endsWith('LEFT');
  const center = placement.endsWith('CENTER');
  return {
    top: top ? 60 : 'auto',
    bottom: top ? 'auto' : 8,
    left: left ? 8 : center ? '50%' : 'auto',
    right: !left && !center ? 8 : 'auto',
    transform: center ? 'translateX(-50%)' : 'none',
  };
}

function motionTransforms(placement: NotificationPlacement) {
  if (placement.endsWith('RIGHT')) {
    return { enter: 'translate3d(14px, 0, 0) scale(0.99)', exit: 'translate3d(8px, 0, 0)' };
  }
  if (placement.endsWith('LEFT')) {
    return {
      enter: 'translate3d(-14px, 0, 0) scale(0.99)',
      exit: 'translate3d(-8px, 0, 0)',
    };
  }
  if (placement.startsWith('TOP')) {
    return { enter: 'translate3d(0, -10px, 0) scale(0.99)', exit: 'translate3d(0, -6px, 0)' };
  }
  return { enter: 'translate3d(0, 10px, 0) scale(0.99)', exit: 'translate3d(0, 6px, 0)' };
}

function severityPresentation(item: CenterNotification) {
  if (item.severity === 'CRITICAL') {
    return { severity: 'error' as const, label: 'Critical', color: 'error.main' };
  }
  if (item.severity === 'WARNING') {
    return { severity: 'warning' as const, label: 'Needs attention', color: 'warning.main' };
  }
  return { severity: 'info' as const, label: 'Information', color: 'info.main' };
}

function cardWrapperStyle(
  mode: NotificationCardMode,
  placement: NotificationPlacement,
  compressedDepth: number,
) {
  if (mode === 'hidden') {
    return {
      position: 'absolute' as const,
      inset: 0,
      width: '100%',
      minWidth: 0,
      visibility: 'hidden' as const,
      pointerEvents: 'none' as const,
    };
  }

  if (mode === 'compressed') {
    const top = placement.startsWith('TOP');
    const offset = compressedDepth * 8;
    return {
      gridArea: '1 / 1',
      alignSelf: top ? ('end' as const) : ('start' as const),
      zIndex: 3 - compressedDepth,
      width: '100%',
      minWidth: 0,
      pointerEvents: 'none' as const,
      transform: `translate3d(0, ${top ? offset : -offset}px, 0) scale(${1 - compressedDepth * 0.025})`,
      transformOrigin: top ? 'bottom center' : 'top center',
      transition: `transform ${DECK_ANIMATION_MS}ms ease, opacity ${DECK_ANIMATION_MS}ms ease`,
    };
  }

  return {
    gridArea: mode === 'primary' ? '1 / 1' : 'auto',
    alignSelf:
      mode === 'primary'
        ? placement.startsWith('TOP')
          ? ('start' as const)
          : ('end' as const)
        : 'auto',
    zIndex: mode === 'primary' ? 4 : 'auto',
    width: '100%',
    minWidth: 0,
    pointerEvents: 'auto' as const,
    transform: 'translate3d(0, 0, 0) scale(1)',
    transition: `transform ${DECK_ANIMATION_MS}ms ease, opacity ${DECK_ANIMATION_MS}ms ease`,
  };
}

interface DeckControl {
  expanded: boolean;
  additionalCount: number;
  onToggle: () => void;
}

function NotificationCard({
  item,
  autoDismissMs,
  placement,
  mode,
  compressedDepth,
  deckControl,
  onClose,
  onOpenTodo,
  onOpenWorkItem,
  onOpenNotifications,
  onInteract,
}: {
  item: CenterNotification;
  autoDismissMs: number | null;
  placement: NotificationPlacement;
  mode: NotificationCardMode;
  compressedDepth: number;
  deckControl?: DeckControl;
  onClose: (notificationId: string) => void;
  onOpenTodo?: (plannerItemId: string) => void;
  onOpenWorkItem?: (workItemId: string, title: string) => void;
  onOpenNotifications?: () => void;
  onInteract?: (notificationId: string) => void;
}) {
  const [exiting, setExiting] = useState(false);
  const [lifetime] = useState<NotificationLifetime | null>(() =>
    autoDismissMs === null ? null : createNotificationLifetime(Date.now(), autoDismissMs),
  );
  const exitTimer = useRef<number | null>(null);
  const exitStarted = useRef(false);

  const beginExit = useCallback(() => {
    if (exitStarted.current) return;
    exitStarted.current = true;
    setExiting(true);
    exitTimer.current = window.setTimeout(() => onClose(item.notificationId), EXIT_ANIMATION_MS);
  }, [item.notificationId, onClose]);

  useEffect(() => {
    if (lifetime === null) return;
    const timer = window.setTimeout(
      beginExit,
      notificationLifetimeRemainingMs(lifetime, Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [beginExit, lifetime]);

  useEffect(
    () => () => {
      if (exitTimer.current !== null) window.clearTimeout(exitTimer.current);
    },
    [],
  );

  const { severity, label: severityLabel, color: severityColor } = severityPresentation(item);
  const motion = motionTransforms(placement);
  const compressed = mode === 'compressed';
  const hidden = mode === 'hidden';

  const handleActionClick = (event: MouseEvent<HTMLElement>) => {
    event.stopPropagation();
    onInteract?.(item.notificationId);
    beginExit();
    if (item.plannerItemId && onOpenTodo) {
      onOpenTodo(item.plannerItemId);
    } else if (item.workItemId && onOpenWorkItem) {
      onOpenWorkItem(item.workItemId, item.workItemTitle ?? 'Work Item');
    } else if (onOpenNotifications) {
      onOpenNotifications();
    }
  };

  const handleDeckToggle = (event: MouseEvent<HTMLElement>) => {
    event.stopPropagation();
    deckControl?.onToggle();
  };

  const actionLabel = item.plannerItemId
    ? 'Open Todo'
    : item.workItemId
      ? 'Open Work Item'
      : 'View Notifications';

  return (
    <Box
      sx={cardWrapperStyle(mode, placement, compressedDepth)}
      aria-hidden={compressed || hidden ? 'true' : undefined}
    >
      <Alert
        severity={severity}
        variant="standard"
        elevation={compressed ? 4 : 8}
        icon={compressed ? false : undefined}
        action={
          !compressed && !hidden ? (
            <IconButton
              size="small"
              aria-label={`Dismiss ${item.title}`}
              color="inherit"
              onClick={beginExit}
              sx={{ mt: -0.25, mr: -0.5 }}
            >
              <CloseRounded fontSize="small" />
            </IconButton>
          ) : undefined
        }
        sx={{
          height: compressed ? 48 : 'auto',
          width: '100%',
          boxSizing: 'border-box',
          alignItems: 'flex-start',
          border: 1,
          borderColor: 'divider',
          borderLeftWidth: 4,
          borderLeftColor: severityColor,
          borderRadius: 2.5,
          bgcolor: 'background.paper',
          color: 'text.primary',
          overflow: 'hidden',
          position: 'relative',
          animation: compressed
            ? 'none'
            : exiting
              ? `notification-exit ${EXIT_ANIMATION_MS}ms ease-in both`
              : `notification-enter ${ENTER_ANIMATION_MS}ms ease-out both`,
          opacity: compressed && exiting ? 0 : 1,
          '@keyframes notification-enter': {
            from: { opacity: 0, transform: motion.enter },
            to: { opacity: 1, transform: 'translate3d(0, 0, 0) scale(1)' },
          },
          '@keyframes notification-exit': {
            from: { opacity: 1, transform: 'translate3d(0, 0, 0) scale(1)' },
            to: { opacity: 0, transform: motion.exit },
          },
          '& .MuiAlert-icon': { mt: 0.25 },
          '& .MuiAlert-message': { minWidth: 0, width: '100%', py: 0.25 },
          '& .MuiAlert-action': { pl: 1 },
        }}
        data-testid={
          compressed ? 'core-in-app-notification-summary' : 'core-in-app-notification-toast'
        }
      >
        <Box sx={{ display: compressed ? 'block' : 'none', minWidth: 0 }}>
          <Typography variant="caption" noWrap sx={{ display: 'block', fontWeight: 700 }}>
            {severityLabel} · {item.title}
          </Typography>
        </Box>
        <Box
          sx={{ display: compressed ? 'none' : 'block', minWidth: 0 }}
          aria-hidden={compressed ? 'true' : undefined}
        >
          <Typography
            variant="overline"
            sx={{ display: 'block', mb: 0.25, fontSize: 10, fontWeight: 750, lineHeight: 1.4 }}
          >
            {severityLabel}
          </Typography>
          <AlertTitle sx={{ mb: 0.5, fontSize: 14, fontWeight: 750, lineHeight: 1.35 }}>
            {item.title}
          </AlertTitle>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ lineHeight: 1.5, overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' }}
          >
            {item.message}
          </Typography>
          <Divider sx={{ my: 1.25 }} />
          <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
            <Button
              variant="contained"
              size="small"
              onClick={handleActionClick}
              startIcon={item.plannerItemId ? <EventNoteRounded /> : <ArrowForwardRounded />}
              sx={{ textTransform: 'none', fontWeight: 700 }}
              data-testid="toast-action-btn"
            >
              {actionLabel}
            </Button>
            {deckControl && (
              <Button
                variant="text"
                size="small"
                onClick={handleDeckToggle}
                startIcon={deckControl.expanded ? <UnfoldLessRounded /> : <UnfoldMoreRounded />}
                aria-expanded={deckControl.expanded}
                aria-controls={NOTIFICATION_LIST_ID}
                aria-label={
                  deckControl.expanded
                    ? 'Collapse notification stack'
                    : `Show ${deckControl.additionalCount} more notifications`
                }
                sx={{ textTransform: 'none', fontWeight: 700 }}
                data-testid="notification-deck-toggle"
              >
                {deckControl.expanded
                  ? 'Collapse stack'
                  : `Show ${deckControl.additionalCount} more`}
              </Button>
            )}
          </Stack>
        </Box>
        {lifetime !== null && (
          <Box
            aria-hidden="true"
            sx={{
              position: 'absolute',
              right: 0,
              bottom: 0,
              left: 0,
              height: 2,
              bgcolor: 'action.hover',
              opacity: compressed || hidden ? 0 : 1,
            }}
          >
            <Box
              sx={{
                width: '100%',
                height: '100%',
                bgcolor: severityColor,
                transformOrigin: 'left center',
                animation: `notification-lifetime ${lifetime.durationMs}ms linear forwards`,
                animationPlayState: exiting ? 'paused' : 'running',
                '@keyframes notification-lifetime': {
                  from: { transform: 'scaleX(1)' },
                  to: { transform: 'scaleX(0)' },
                },
              }}
            />
          </Box>
        )}
      </Alert>
    </Box>
  );
}

export default function NotificationToast({
  items,
  settings,
  onClose,
  onOpenTodo,
  onOpenWorkItem,
  onOpenNotifications,
  onInteract,
}: NotificationToastProps) {
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    if (!expanded || items.length > COMPACT_DECK_THRESHOLD) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset local deck UI after it leaves compact mode
    setExpanded(false);
  }, [expanded, items.length]);

  if (items.length === 0) return null;

  const autoDismissMs = settings.autoDismiss ? settings.autoDismissSeconds * 1000 : null;
  const deck = resolveNotificationDeck(items.length, expanded);
  const topPlacement = settings.placement.startsWith('TOP');

  return (
    <Portal>
      <Box
        sx={{
          position: 'fixed',
          zIndex: (theme) => theme.zIndex.snackbar,
          boxSizing: 'border-box',
          width: 'min(432px, calc(100vw - 16px))',
          maxHeight: 'calc(100vh - 68px)',
          p: 2,
          overflowY: 'auto',
          overscrollBehavior: 'contain',
          scrollbarWidth: 'thin',
          pointerEvents: 'none',
          ...placementStyle(settings.placement),
        }}
        data-testid="core-in-app-notification-stack"
      >
        <Box
          id={NOTIFICATION_LIST_ID}
          role="region"
          aria-label={`${items.length} in-app notification${items.length === 1 ? '' : 's'}`}
          sx={{
            display: deck.compact && !deck.expanded ? 'grid' : 'flex',
            flexDirection: topPlacement ? 'column' : 'column-reverse',
            gap: deck.compact && !deck.expanded ? 0 : 2,
            pt: deck.compact && !deck.expanded && !topPlacement ? 2 : 0,
            pb: deck.compact && !deck.expanded && topPlacement ? 2 : 0,
            position: 'relative',
            minWidth: 0,
            pointerEvents: 'none',
            animation: deck.compact
              ? deck.expanded
                ? `notification-deck-expand ${DECK_ANIMATION_MS}ms ease-out both`
                : `notification-deck-collapse ${DECK_ANIMATION_MS}ms ease-out both`
              : 'none',
            '@keyframes notification-deck-expand': {
              from: { opacity: 0.88, transform: 'translate3d(0, 4px, 0)' },
              to: { opacity: 1, transform: 'translate3d(0, 0, 0)' },
            },
            '@keyframes notification-deck-collapse': {
              from: { opacity: 0.9, transform: 'translate3d(0, -3px, 0)' },
              to: { opacity: 1, transform: 'translate3d(0, 0, 0)' },
            },
          }}
        >
          {items.map((item, index) => {
            const mode = deck.modes[index] ?? 'hidden';
            const deckControl =
              index === 0 && deck.compact
                ? {
                    expanded: deck.expanded,
                    additionalCount: deck.additionalCount,
                    onToggle: () => setExpanded((current) => !current),
                  }
                : undefined;
            return (
              <NotificationCard
                key={`${item.notificationId}:${autoDismissMs ?? 'persistent'}`}
                item={item}
                autoDismissMs={autoDismissMs}
                placement={settings.placement}
                mode={mode}
                compressedDepth={mode === 'compressed' ? index : 0}
                deckControl={deckControl}
                onClose={onClose}
                onOpenTodo={onOpenTodo}
                onOpenWorkItem={onOpenWorkItem}
                onOpenNotifications={onOpenNotifications}
                onInteract={onInteract}
              />
            );
          })}
        </Box>
      </Box>
    </Portal>
  );
}
