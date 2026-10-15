import AccessTimeRounded from '@mui/icons-material/AccessTimeRounded';
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded';
import ExpandLessRounded from '@mui/icons-material/ExpandLessRounded';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import InfoOutlined from '@mui/icons-material/InfoOutlined';
import NotificationsActiveOutlined from '@mui/icons-material/NotificationsActiveOutlined';
import WarningAmberRounded from '@mui/icons-material/WarningAmberRounded';
import { Box, Button, Chip, Collapse, IconButton, Stack, Typography } from '@mui/material';
import { alpha } from '@mui/material/styles';
import type { SvgIconComponent } from '@mui/icons-material';
import type { KeyboardEvent, MouseEvent } from 'react';
import type {
  CenterNotification,
  NotificationDisposition,
} from '../../api/notifications/notificationsProductApi.ts';
import {
  buildHistoryEntries,
  formatRelativeTime,
  severityStyleKey,
  type SeverityStyleKey,
} from '../../notifications/notificationCenterModel.ts';

interface NotificationRowProps {
  item: CenterNotification;
  now: Date;
  busy: boolean;
  expanded: boolean;
  onToggle: () => void;
  onOpenItem: () => void;
  onOpenSnooze: (anchor: HTMLElement) => void;
}

interface SeverityStyle {
  label: string;
  icon: SvgIconComponent;
  color: string;
  background: string;
}

const severityStyles: Record<SeverityStyleKey, SeverityStyle> = {
  high: {
    label: 'High',
    icon: WarningAmberRounded,
    color: 'error.main',
    background: 'warning.light',
  },
  attention: {
    label: 'Attention',
    icon: NotificationsActiveOutlined,
    color: 'primary.main',
    background: 'primary.light',
  },
  info: {
    label: 'Info',
    icon: InfoOutlined,
    color: 'text.secondary',
    background: 'surface.secondary',
  },
};

const dispositionLabels: Record<NotificationDisposition, string> = {
  NEEDS_ACTION: 'Needs action',
  SNOOZED: 'Snoozed',
  RESOLVED: 'Resolved',
};

function dispositionTone(disposition: NotificationDisposition) {
  if (disposition === 'NEEDS_ACTION') return 'warning';
  if (disposition === 'RESOLVED') return 'success';
  return 'default';
}

export default function NotificationRow({
  item,
  now,
  busy,
  expanded,
  onToggle,
  onOpenItem,
  onOpenSnooze,
}: NotificationRowProps) {
  const styleKey = severityStyleKey(item.severity);
  const severity = severityStyles[styleKey];
  const SeverityIcon = severity.icon;
  const resolved = item.disposition === 'RESOLVED';
  const unread = item.readAt === null;
  const timeLabel = formatRelativeTime(item.sentAt, now);
  const relatedItem = item.workItemTitle;
  const snoozedLabel =
    item.disposition === 'SNOOZED' && item.snoozedUntil
      ? `Snoozed until ${new Date(item.snoozedUntil).toLocaleString(undefined, {
          month: 'short',
          day: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
        })}`
      : null;

  const handleRowKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.target !== event.currentTarget) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onToggle();
    }
  };

  const stopAndRun = (event: MouseEvent<HTMLElement>, action: () => void) => {
    event.stopPropagation();
    action();
  };

  return (
    <Box
      component="article"
      tabIndex={0}
      aria-expanded={expanded}
      aria-label={`${item.title}. ${expanded ? 'Collapse' : 'Expand'} notification details`}
      onClick={onToggle}
      onKeyDown={handleRowKeyDown}
      sx={(theme) => ({
        display: 'grid',
        gridTemplateColumns: '36px minmax(0, 1fr) 26px',
        gridTemplateAreas: '"icon content disclosure"',
        columnGap: 1.25,
        minWidth: 0,
        px: 'clamp(14px, 1.8cqw, 22px)',
        py: 1.5,
        borderBottom: 1,
        borderBottomColor: 'divider',
        borderLeft: 2,
        borderLeftColor: styleKey === 'info' ? 'divider' : severity.color,
        bgcolor: expanded
          ? 'surface.secondary'
          : resolved
            ? 'surface.subtle'
            : unread
              ? alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.055 : 0.04)
              : 'background.paper',
        boxShadow: expanded ? `inset 0 0 0 1px ${theme.palette.divider}` : 'none',
        cursor: 'pointer',
        transition: 'background-color 120ms ease',
        '@container (min-width: 720px)': {
          gridTemplateColumns: '40px minmax(0, 1fr) auto',
          gridTemplateAreas: '"icon content disclosure"',
          columnGap: 1.5,
        },
        '&:hover': { bgcolor: 'surface.secondary' },
        '&:focus-visible': {
          zIndex: 1,
          outline: '2px solid',
          outlineColor: 'primary.main',
          outlineOffset: -2,
        },
      })}
    >
      <Box
        sx={{
          gridArea: 'icon',
          display: 'grid',
          placeItems: 'center',
          width: 34,
          height: 34,
          borderRadius: 1,
          color: severity.color,
          bgcolor: severity.background,
          '& svg': { fontSize: 18 },
        }}
      >
        <SeverityIcon />
      </Box>

      <Box sx={{ gridArea: 'content', minWidth: 0 }}>
        <Stack
          direction="row"
          spacing={0.75}
          useFlexGap
          sx={{ minWidth: 0, alignItems: 'center', flexWrap: 'wrap' }}
        >
          {unread && (
            <Box
              aria-label="Unread"
              sx={{
                width: 6,
                height: 6,
                flexShrink: 0,
                borderRadius: '50%',
                bgcolor: 'primary.main',
              }}
            />
          )}
          <Typography
            component="h3"
            sx={{
              minWidth: 0,
              color: resolved ? 'text.secondary' : 'text.primary',
              fontSize: 13,
              fontWeight: unread ? 700 : 600,
            }}
          >
            {item.title}
          </Typography>
          <Typography
            variant="caption"
            sx={{ color: severity.color, fontWeight: 700, textTransform: 'uppercase' }}
          >
            {severity.label}
          </Typography>
          <Chip
            label={dispositionLabels[item.disposition]}
            size="small"
            color={dispositionTone(item.disposition)}
            variant="outlined"
            icon={
              item.disposition === 'RESOLVED' ? (
                <CheckCircleOutlineRounded />
              ) : item.disposition === 'SNOOZED' ? (
                <AccessTimeRounded />
              ) : undefined
            }
            sx={{ height: 21, fontSize: 9, '& .MuiChip-icon': { fontSize: 13 } }}
          />
        </Stack>

        <Typography
          color="text.secondary"
          sx={{
            mt: 0.45,
            display: '-webkit-box',
            overflow: 'hidden',
            WebkitBoxOrient: 'vertical',
            WebkitLineClamp: 2,
          }}
        >
          {item.message}
        </Typography>

        <Stack
          direction="row"
          spacing={0.75}
          useFlexGap
          sx={{ mt: 0.6, minWidth: 0, alignItems: 'center', flexWrap: 'wrap' }}
        >
          {relatedItem && (
            <Typography variant="caption" color="text.secondary" noWrap>
              {relatedItem}
            </Typography>
          )}
          {relatedItem && (
            <Typography variant="caption" color="text.secondary" aria-hidden="true">
              ·
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary" noWrap>
            Lum Harbor Attention
          </Typography>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'inline', '@container (min-width: 720px)': { display: 'none' } }}
          >
            · {timeLabel}
          </Typography>
        </Stack>

        {snoozedLabel && (
          <Stack
            direction="row"
            spacing={0.5}
            sx={{ mt: 0.75, alignItems: 'center', color: 'warning.main' }}
          >
            <AccessTimeRounded sx={{ fontSize: 14 }} />
            <Typography variant="caption" sx={{ fontWeight: 650 }}>
              {snoozedLabel}
            </Typography>
          </Stack>
        )}

        <Stack
          direction="row"
          spacing={0.75}
          useFlexGap
          sx={{ mt: 1.05, alignItems: 'center', flexWrap: 'wrap' }}
        >
          <Button
            size="small"
            variant={resolved ? 'text' : 'outlined'}
            onClick={(event) => stopAndRun(event, onOpenItem)}
          >
            {item.plannerItemId ? 'Open Todo' : 'Open Item'}
          </Button>
          {item.disposition === 'NEEDS_ACTION' && (
            <Button
              size="small"
              color="inherit"
              disabled={busy}
              onClick={(event) => {
                event.stopPropagation();
                onOpenSnooze(event.currentTarget);
              }}
              sx={{ color: 'text.secondary' }}
            >
              Snooze
            </Button>
          )}
        </Stack>

        <Collapse in={expanded} timeout={160} unmountOnExit>
          <Box
            sx={{
              display: 'grid',
              gridTemplateColumns: 'minmax(0, 1fr)',
              gap: 1.75,
              mt: 1.5,
              pt: 1.5,
              borderTop: 1,
              borderColor: 'divider',
              '@container (min-width: 760px)': {
                gridTemplateColumns: 'minmax(0, 1.35fr) minmax(220px, 1fr)',
              },
            }}
          >
            <Box sx={{ minWidth: 0 }}>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase' }}
              >
                Why you were notified
              </Typography>
              <Typography sx={{ mt: 0.55 }}>{item.message}</Typography>
              {item.recommendedAction && (
                <Typography color="text.secondary" sx={{ mt: 0.5 }}>
                  {item.recommendedAction}
                </Typography>
              )}
              <Box
                sx={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
                  gap: 1.5,
                  mt: 1.5,
                }}
              >
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="caption" color="text.secondary">
                    Related Item
                  </Typography>
                  <Typography sx={{ mt: 0.25, fontWeight: 600 }} noWrap>
                    {relatedItem ?? 'None'}
                  </Typography>
                </Box>
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="caption" color="text.secondary">
                    Source
                  </Typography>
                  <Typography sx={{ mt: 0.25, fontWeight: 600 }} noWrap>
                    Lum Harbor Attention
                  </Typography>
                </Box>
              </Box>
            </Box>

            <Box sx={{ minWidth: 0 }}>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase' }}
              >
                Notification History
              </Typography>
              <Stack spacing={0.8} sx={{ mt: 0.65 }}>
                {buildHistoryEntries(item).map((entry) => (
                  <Stack key={entry.id} direction="row" spacing={1} sx={{ minWidth: 0 }}>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ width: 82, flexShrink: 0, fontVariantNumeric: 'tabular-nums' }}
                    >
                      {entry.label}
                    </Typography>
                    <Typography variant="body2" sx={{ minWidth: 0, fontWeight: 550 }}>
                      {entry.description}
                    </Typography>
                  </Stack>
                ))}
              </Stack>
            </Box>
          </Box>
        </Collapse>
      </Box>

      <Stack
        direction="row"
        spacing={0.25}
        sx={{
          gridArea: 'disclosure',
          alignItems: 'flex-start',
          justifyContent: 'flex-end',
          color: 'text.secondary',
        }}
      >
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{
            display: 'none',
            pt: 0.2,
            whiteSpace: 'nowrap',
            fontVariantNumeric: 'tabular-nums',
            '@container (min-width: 720px)': { display: 'block' },
          }}
        >
          {timeLabel}
        </Typography>
        <IconButton
          size="small"
          onClick={(event) => stopAndRun(event, onToggle)}
          aria-label={expanded ? `Collapse ${item.title}` : `Expand ${item.title}`}
          sx={{ mt: -0.55, mr: -0.55, color: 'text.secondary' }}
        >
          {expanded ? (
            <ExpandLessRounded fontSize="small" />
          ) : (
            <ExpandMoreRounded fontSize="small" />
          )}
        </IconButton>
      </Stack>
    </Box>
  );
}
