import { Box, Stack, Typography } from '@mui/material';

export default function WorkflowLegend() {
  return (
    <Box
      sx={{
        py: 0.25,
        display: 'flex',
        flexWrap: 'wrap',
        gap: 2,
        alignItems: 'center',
      }}
    >
      {/* Node Shapes (Item Types) */}
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
        <Typography variant="caption" sx={{ fontWeight: 700, color: 'text.secondary', mr: 0.5 }}>
          TYPES:
        </Typography>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <svg width="12" height="12">
            <circle cx="6" cy="6" r="4.5" fill="currentColor" />
          </svg>
          <Typography variant="caption" color="text.secondary">
            Story
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <svg width="12" height="12">
            <rect x="2" y="2" width="8" height="8" rx="1.5" fill="currentColor" />
          </svg>
          <Typography variant="caption" color="text.secondary">
            Task
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <svg width="12" height="12">
            <polygon points="6,2 11,10 1,10" fill="currentColor" />
          </svg>
          <Typography variant="caption" color="text.secondary">
            Defect
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <svg width="12" height="12">
            <polygon points="6,1 11,6 6,11 1,6" fill="currentColor" />
          </svg>
          <Typography variant="caption" color="text.secondary">
            Gate
          </Typography>
        </Stack>
      </Stack>

      {/* Health Colors */}
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
        <Typography variant="caption" sx={{ fontWeight: 700, color: 'text.secondary', mr: 0.5 }}>
          HEALTH:
        </Typography>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: 'success.main' }} />
          <Typography variant="caption" color="text.secondary">
            On Track
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: 'warning.main' }} />
          <Typography variant="caption" color="text.secondary">
            At Risk
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: 'error.main' }} />
          <Typography variant="caption" color="text.secondary">
            Blocked
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: 'text.disabled' }} />
          <Typography variant="caption" color="text.secondary">
            Queued
          </Typography>
        </Stack>
      </Stack>

      {/* Target & Dependency Semantics */}
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <svg width="12" height="12">
            <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeDasharray="2,2" />
          </svg>
          <Typography variant="caption" color="text.secondary">
            Target Ghost
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Box sx={{ width: 14, height: 2, bgcolor: 'primary.main', borderRadius: 1 }} />
          <Typography variant="caption" color="text.secondary">
            Trunk Baseline
          </Typography>
        </Stack>
      </Stack>
    </Box>
  );
}
