import { Box, Stack, Typography } from '@mui/material';
import type { ReactNode } from 'react';

interface PageHeaderProps {
  eyebrow: string;
  title: string;
  subtitle: string;
  status?: string;
  action?: ReactNode;
}

export default function PageHeader({ eyebrow, title, subtitle, status, action }: PageHeaderProps) {
  return (
    <Stack
      direction="row"
      spacing={2}
      sx={{ mb: 3, minWidth: 0, alignItems: 'flex-start', justifyContent: 'space-between' }}
    >
      <Box sx={{ minWidth: 0 }}>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ fontWeight: 650, letterSpacing: '0.1em', textTransform: 'uppercase' }}
        >
          {eyebrow}
        </Typography>
        <Typography component="h1" variant="h1" sx={{ mt: 0.5 }}>
          {title}
        </Typography>
        <Typography color="text.secondary" sx={{ mt: 0.75 }}>
          {subtitle}
        </Typography>
        {status && (
          <Stack
            direction="row"
            spacing={0.8}
            sx={{ mt: 1.1, minWidth: 0, alignItems: 'center', color: 'text.secondary' }}
          >
            <Box
              aria-hidden="true"
              sx={{
                flex: '0 0 auto',
                width: 5,
                height: 5,
                borderRadius: '50%',
                bgcolor: 'primary.main',
                opacity: 0.65,
              }}
            />
            <Typography variant="caption">{status}</Typography>
          </Stack>
        )}
      </Box>
      {action && <Box sx={{ flexShrink: 0 }}>{action}</Box>}
    </Stack>
  );
}
