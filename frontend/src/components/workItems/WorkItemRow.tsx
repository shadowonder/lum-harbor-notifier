import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import { Box, Chip, Paper, Stack, Typography } from '@mui/material';
import type { ProductWorkItem, ProductWorkItemSummary } from '../../api/workItems/productApi.ts';
import { buildOverviewRowView } from '../../presentation/workItems/workItemOverviewPresentation.ts';
import {
  OverviewAttentionText,
  OverviewRollup,
  OverviewTargetBlock,
  OverviewTrackingCenter,
} from './overview/OverviewSummaryBlocks.tsx';
import {
  buildColumnDividerBorderColor,
  buildRowGridTemplateColumns,
} from './currentSummary/rowLayoutContract.ts';

interface WorkItemRowProps {
  item: ProductWorkItem;
  onOpenWorkspace: (id: string) => void;
  summary?: ProductWorkItemSummary;
  pendingProposalCount?: number;
}

export default function WorkItemRow({
  item,
  onOpenWorkspace,
  summary,
  pendingProposalCount,
}: WorkItemRowProps) {
  const view = buildOverviewRowView(summary ?? { tracking: null, attention: null }, new Date());
  const isClosed = item.lifecycleState === 'COMPLETED' || item.lifecycleState === 'CANCELLED';
  const activate = () => onOpenWorkspace(item.id);
  return (
    <Paper
      component="li"
      variant="outlined"
      role="button"
      tabIndex={0}
      aria-label={`Open ${item.title} workspace`}
      onClick={activate}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          activate();
        }
      }}
      sx={{
        listStyle: 'none',
        overflow: 'hidden',
        cursor: 'pointer',
        borderRadius: 1,
        bgcolor: 'background.paper',
        opacity: isClosed ? 0.86 : 1,
        borderColor: 'divider',
        transition: 'background-color 140ms ease, border-color 140ms ease',
        '&:hover': {
          bgcolor: 'action.hover',
          borderColor: 'text.disabled',
          '& .secondary-arrow': { transform: 'translateX(3px)', color: 'primary.main' },
        },
        '&:focus-visible': {
          outline: '2px solid',
          outlineColor: 'primary.main',
          outlineOffset: -2,
        },
      }}
    >
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', md: buildRowGridTemplateColumns() },
          minHeight: 142,
          alignItems: 'stretch',
        }}
      >
        <Box
          sx={{
            p: 1.5,
            display: 'flex',
            flexDirection: 'column',
            gap: 0.7,
            minWidth: 0,
            borderRight: { md: 1 },
            borderColor: { md: (theme) => buildColumnDividerBorderColor(theme) },
          }}
        >
          <Typography
            component="h3"
            variant="h3"
            title={item.title}
            color={isClosed ? 'text.secondary' : 'text.primary'}
            sx={{
              display: '-webkit-box',
              WebkitLineClamp: 3,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
              maxWidth: '100%',
              lineHeight: 1.18,
            }}
          >
            {item.title}
          </Typography>
          <Stack direction="row" spacing={0.65} sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
            <Chip
              size="small"
              label={item.lifecycleState}
              variant="outlined"
              color={
                item.lifecycleState === 'COMPLETED'
                  ? 'success'
                  : item.lifecycleState === 'PAUSED'
                    ? 'warning'
                    : item.lifecycleState === 'ACTIVE'
                      ? 'primary'
                      : 'default'
              }
              sx={{ height: 20, '& .MuiChip-label': { px: 0.75, fontSize: 10.5 } }}
            />
          </Stack>
          <OverviewTargetBlock target={view.target} />
        </Box>

        <Box
          sx={{
            p: 1.35,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            minWidth: 0,
            overflow: 'hidden',
            borderRight: { md: 1 },
            borderColor: { md: (theme) => buildColumnDividerBorderColor(theme) },
          }}
        >
          <OverviewTrackingCenter tracking={view.tracking} />
        </Box>

        <Box sx={{ p: 1.75, display: 'flex', flexDirection: 'column', gap: 1.1, minWidth: 0 }}>
          <Box sx={{ minWidth: 0 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontWeight: 650, letterSpacing: '0.06em' }}
            >
              SUMMARY
            </Typography>
            <Typography
              variant="body2"
              color={item.description ? 'text.primary' : 'text.secondary'}
              title={item.description ?? undefined}
              sx={{
                display: '-webkit-box',
                WebkitLineClamp: 4,
                WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
                lineHeight: 1.4,
              }}
            >
              {item.description ?? 'No summary provided'}
            </Typography>
          </Box>
          <Box sx={{ minWidth: 0 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontWeight: 650, letterSpacing: '0.06em' }}
            >
              TRACKED WORK
            </Typography>
            <OverviewRollup tracking={view.tracking} />
          </Box>
          <Box sx={{ minWidth: 0 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontWeight: 650, letterSpacing: '0.06em' }}
            >
              ATTENTION
            </Typography>
            <OverviewAttentionText attention={view.attention} />
            {pendingProposalCount !== undefined && pendingProposalCount > 0 && (
              <Typography variant="body2" color="text.secondary" sx={{ display: 'block' }}>
                {pendingProposalCount === 1
                  ? '1 suggestion awaiting review'
                  : `${pendingProposalCount} suggestions awaiting review`}
              </Typography>
            )}
          </Box>
          <Stack
            direction="row"
            spacing={0.5}
            sx={{
              mt: 'auto',
              alignItems: 'center',
              justifyContent: 'flex-end',
              color: 'text.secondary',
            }}
          >
            <Typography variant="caption" sx={{ fontWeight: 650 }}>
              Workspace
            </Typography>
            <ArrowForwardRounded
              className="secondary-arrow"
              sx={{ fontSize: '0.9rem', transition: 'transform 140ms ease, color 140ms ease' }}
            />
          </Stack>
        </Box>
      </Box>
    </Paper>
  );
}
