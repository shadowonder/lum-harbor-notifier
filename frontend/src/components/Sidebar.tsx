import CalendarMonthRounded from '@mui/icons-material/CalendarMonthRounded';
import ChevronLeftRounded from '@mui/icons-material/ChevronLeftRounded';
import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import DashboardRounded from '@mui/icons-material/DashboardRounded';
import HistoryRounded from '@mui/icons-material/HistoryRounded';
import NotificationsNoneRounded from '@mui/icons-material/NotificationsNoneRounded';
import SettingsRounded from '@mui/icons-material/SettingsRounded';
import AssignmentOutlined from '@mui/icons-material/AssignmentOutlined';
import {
  Box,
  Divider,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Tooltip,
  Typography,
} from '@mui/material';
import type { SvgIconComponent } from '@mui/icons-material';

export type PageId =
  'dashboard' | 'planner' | 'workItems' | 'notifications' | 'history' | 'settings';

interface SidebarProps {
  activePage: PageId;
  collapsed: boolean;
  onNavigate: (page: PageId) => void;
  onToggleCollapsed: () => void;
}

const primaryItems: { id: PageId; label: string; icon: SvgIconComponent }[] = [
  { id: 'dashboard', label: 'Dashboard', icon: DashboardRounded },
  { id: 'planner', label: 'Planner', icon: CalendarMonthRounded },
  { id: 'workItems', label: 'Work Items', icon: AssignmentOutlined },
  { id: 'notifications', label: 'Notifications', icon: NotificationsNoneRounded },
  { id: 'history', label: 'History', icon: HistoryRounded },
];

function NavigationItem({
  id,
  label,
  icon: Icon,
  selected,
  collapsed,
  onNavigate,
}: {
  id: PageId;
  label: string;
  icon: SvgIconComponent;
  selected: boolean;
  collapsed: boolean;
  onNavigate: (page: PageId) => void;
}) {
  return (
    <Tooltip
      title={collapsed ? label : ''}
      placement="right"
      disableHoverListener={!collapsed}
      disableFocusListener={!collapsed}
      disableTouchListener={!collapsed}
    >
      <ListItemButton
        selected={selected}
        onClick={() => onNavigate(id)}
        aria-current={selected ? 'page' : undefined}
        sx={{
          minHeight: 40,
          mx: 1,
          mb: 0.5,
          px: collapsed ? 0 : { xs: 1.25, lg: 1.5 },
          borderRadius: 1,
          justifyContent: collapsed ? 'center' : 'flex-start',
          color: selected ? 'primary.dark' : 'text.secondary',
          '&.Mui-selected': { bgcolor: 'primary.light' },
          '&.Mui-selected:hover': { bgcolor: 'primary.light' },
        }}
      >
        <ListItemIcon
          sx={{ minWidth: collapsed ? 0 : 35, color: 'inherit', justifyContent: 'center' }}
        >
          <Icon sx={{ fontSize: 19 }} />
        </ListItemIcon>
        <ListItemText
          sx={{
            m: 0,
            maxWidth: collapsed ? 0 : 160,
            overflow: 'hidden',
            opacity: collapsed ? 0 : 1,
            transition: 'opacity 160ms ease',
          }}
          primary={
            <Typography sx={{ fontSize: 12, fontWeight: selected ? 650 : 500 }} noWrap>
              {label}
            </Typography>
          }
        />
      </ListItemButton>
    </Tooltip>
  );
}

export default function Sidebar({
  activePage,
  collapsed,
  onNavigate,
  onToggleCollapsed,
}: SidebarProps) {
  return (
    <Box
      component="aside"
      aria-label="Application sidebar"
      sx={{
        display: 'flex',
        flexDirection: 'column',
        minHeight: 0,
        overflowX: 'hidden',
        overflowY: 'auto',
        bgcolor: 'surface.sidebar',
        borderRight: 1,
        borderColor: 'divider',
      }}
    >
      <Box component="nav" aria-label="Main navigation" sx={{ pt: 2.25 }}>
        <Box
          sx={{
            display: 'flex',
            height: 32,
            px: collapsed ? 1 : 1.5,
            mb: 0.75,
            alignItems: 'center',
            justifyContent: collapsed ? 'center' : 'space-between',
          }}
        >
          {!collapsed && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ pl: 1, fontWeight: 650, letterSpacing: '0.09em', textTransform: 'uppercase' }}
            >
              Workspace
            </Typography>
          )}
          <Tooltip title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} placement="right">
            <IconButton
              size="small"
              onClick={onToggleCollapsed}
              aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
              sx={{ color: 'text.secondary' }}
            >
              {collapsed ? (
                <ChevronRightRounded fontSize="small" />
              ) : (
                <ChevronLeftRounded fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        </Box>
        <List disablePadding>
          {primaryItems.map((item) => (
            <NavigationItem
              key={item.id}
              {...item}
              selected={activePage === item.id}
              collapsed={collapsed}
              onNavigate={onNavigate}
            />
          ))}
        </List>
      </Box>
      <Box sx={{ mt: 'auto' }}>
        <Divider />
        <List disablePadding sx={{ py: 1 }}>
          <NavigationItem
            id="settings"
            label="Settings"
            icon={SettingsRounded}
            selected={activePage === 'settings'}
            collapsed={collapsed}
            onNavigate={onNavigate}
          />
        </List>
      </Box>
    </Box>
  );
}
