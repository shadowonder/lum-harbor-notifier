import AutoAwesomeRounded from '@mui/icons-material/AutoAwesomeRounded';
import { Box, Button, Stack, Typography } from '@mui/material';
import type { ProductProposal } from '../../api/proposals/proposalProductApi.ts';

export default function PreviewBanner({
  proposal,
  onExit,
}: {
  proposal: ProductProposal;
  onExit: () => void;
}) {
  return (
    <Box
      role="status"
      sx={{
        position: 'sticky',
        top: 0,
        zIndex: 20,
        px: 'clamp(16px, 3.2vw, 48px)',
        py: 0.8,
        borderBottom: 1,
        borderBottomStyle: 'dashed',
        borderColor: '#7969C7',
        bgcolor: (theme) =>
          theme.palette.mode === 'dark' ? 'rgba(76, 65, 118, .94)' : 'rgba(245, 242, 255, .96)',
        backdropFilter: 'blur(10px)',
      }}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ maxWidth: 1840, mx: 'auto', alignItems: 'center', justifyContent: 'space-between' }}
      >
        <Stack direction="row" spacing={1} sx={{ minWidth: 0, alignItems: 'center' }}>
          <AutoAwesomeRounded sx={{ flexShrink: 0, fontSize: 17, color: '#7969C7' }} />
          <Box sx={{ minWidth: 0 }}>
            <Typography
              variant="caption"
              sx={{
                display: 'block',
                fontWeight: 760,
                letterSpacing: '.07em',
                textTransform: 'uppercase',
              }}
            >
              Previewing proposed change
            </Typography>
            <Typography variant="body2" color="text.secondary" noWrap>
              {proposal.title} · not applied
            </Typography>
          </Box>
        </Stack>
        <Button size="small" color="inherit" onClick={onExit} sx={{ flexShrink: 0 }}>
          Exit Preview
        </Button>
      </Stack>
    </Box>
  );
}
