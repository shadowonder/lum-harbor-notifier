import { Box } from '@mui/material';
import type { ReactNode } from 'react';

interface PageContainerProps {
  children: ReactNode;
}

export default function PageContainer({ children }: PageContainerProps) {
  return (
    <Box sx={{ width: '100%', maxWidth: 1840, mx: 'auto', p: 'clamp(24px, 3.2vw, 56px)' }}>
      {children}
    </Box>
  );
}
