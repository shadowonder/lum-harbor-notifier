import { useState } from 'react';
import AutoAwesomeRounded from '@mui/icons-material/AutoAwesomeRounded';
import BrightnessAutoRounded from '@mui/icons-material/BrightnessAutoRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import DarkModeRounded from '@mui/icons-material/DarkModeRounded';
import LightModeRounded from '@mui/icons-material/LightModeRounded';
import NotificationsNoneRounded from '@mui/icons-material/NotificationsNoneRounded';
import SailingRounded from '@mui/icons-material/SailingRounded';
import {
  Badge,
  Box,
  Button,
  Divider,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import type { AppearanceMode } from '../appearance';
import { useAppearance } from '../appearance';

interface HeaderProps {
  onOpenAssistant: () => void;
  onOpenNotifications?: () => void;
  unreadCount?: number;
}

const currentDate = new Intl.DateTimeFormat('en-US', {
  weekday: 'long',
  month: 'long',
  day: 'numeric',
}).format(new Date());

export default function Header({
  onOpenAssistant,
  onOpenNotifications,
  unreadCount = 0,
}: HeaderProps) {
  const { appearanceMode, setAppearanceMode } = useAppearance();
  const [appearanceAnchor, setAppearanceAnchor] = useState<HTMLElement | null>(null);
  const AppearanceIcon =
    appearanceMode === 'system'
      ? BrightnessAutoRounded
      : appearanceMode === 'dark'
        ? DarkModeRounded
        : LightModeRounded;

  const selectAppearance = (mode: AppearanceMode) => {
    setAppearanceMode(mode);
    setAppearanceAnchor(null);
  };

  return (
    <Box
      component="header"
      sx={{
        gridColumn: '1 / -1',
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        minWidth: 0,
        px: { xs: 2, lg: 2.75 },
        bgcolor: 'background.paper',
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      <Stack direction="row" spacing={1.15} sx={{ minWidth: 0, alignItems: 'center' }}>
        <Box
          sx={{
            display: 'grid',
            placeItems: 'center',
            width: 30,
            height: 30,
            borderRadius: 1.25,
            bgcolor: 'primary.main',
            color: 'common.white',
          }}
        >
          <SailingRounded sx={{ fontSize: 19 }} />
        </Box>
        <Typography
          sx={{ fontSize: 15, fontWeight: 650, letterSpacing: '-0.025em', whiteSpace: 'nowrap' }}
        >
          Lum Harbor
        </Typography>
      </Stack>

      <Typography
        variant="body2"
        color="text.secondary"
        component="time"
        sx={{ ml: 'auto', whiteSpace: 'nowrap' }}
      >
        {currentDate}
      </Typography>

      <Divider orientation="vertical" flexItem sx={{ my: 2 }} />
      <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
        <Tooltip title={unreadCount > 0 ? `${unreadCount} unread notification${unreadCount === 1 ? '' : 's'}` : 'Notifications'}>
          <IconButton
            size="small"
            onClick={onOpenNotifications}
            aria-label="Notifications"
            data-testid="header-notifications-btn"
          >
            <Badge
              badgeContent={unreadCount > 0 ? unreadCount : undefined}
              color="warning"
              variant={unreadCount > 0 ? 'standard' : 'dot'}
              invisible={unreadCount === 0}
            >
              <NotificationsNoneRounded fontSize="small" />
            </Badge>
          </IconButton>
        </Tooltip>
        <Tooltip title={`Appearance: ${appearanceMode}`}>
          <IconButton
            size="small"
            onClick={(event) => setAppearanceAnchor(event.currentTarget)}
            aria-label={`Appearance: ${appearanceMode}`}
            aria-controls={appearanceAnchor ? 'appearance-menu' : undefined}
            aria-haspopup="menu"
            aria-expanded={appearanceAnchor ? 'true' : undefined}
          >
            <AppearanceIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <Menu
          id="appearance-menu"
          anchorEl={appearanceAnchor}
          open={Boolean(appearanceAnchor)}
          onClose={() => setAppearanceAnchor(null)}
          slotProps={{ paper: { sx: { minWidth: 168 } } }}
        >
          {(
            [
              ['system', 'System', BrightnessAutoRounded],
              ['light', 'Light', LightModeRounded],
              ['dark', 'Dark', DarkModeRounded],
            ] as const
          ).map(([mode, label, Icon]) => (
            <MenuItem
              key={mode}
              selected={appearanceMode === mode}
              onClick={() => selectAppearance(mode)}
            >
              <ListItemIcon>
                <Icon fontSize="small" />
              </ListItemIcon>
              <ListItemText>{label}</ListItemText>
              {appearanceMode === mode && (
                <CheckRounded sx={{ ml: 2, fontSize: 17, color: 'primary.main' }} />
              )}
            </MenuItem>
          ))}
        </Menu>
        <Button
          variant="outlined"
          size="small"
          startIcon={<AutoAwesomeRounded sx={{ fontSize: '16px !important' }} />}
          onClick={onOpenAssistant}
          sx={{ color: 'text.primary', borderColor: 'divider', whiteSpace: 'nowrap' }}
        >
          AI Assistant
        </Button>
      </Stack>
    </Box>
  );
}
