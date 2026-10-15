import { useEffect, useState } from 'react';
import { Box, Button, Paper, Stack, Typography } from '@mui/material';
import AppShell from './components/AppShell';
import { getRuntimeConfig, type RuntimeConfig } from './api/runtimeConfig';

interface RuntimeConfigState {
  data: RuntimeConfig | null;
  error: string | null;
  generation: number;
}

/** Converts bridge failures into stable text for the initialization surface. */
function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Loads the immutable Runtime Config once at application startup. AppShell is
 * not mounted until the Wails contract succeeds, preventing UI surfaces from
 * silently falling back when configuration or bridge initialization fails.
 */
function App() {
  const [state, setState] = useState<RuntimeConfigState>({
    data: null,
    error: null,
    generation: 0,
  });

  useEffect(() => {
    let active = true;
    void getRuntimeConfig().then(
      (data) => {
        if (active) setState((current) => ({ ...current, data, error: null }));
      },
      (error: unknown) => {
        if (active) setState((current) => ({ ...current, error: errorMessage(error) }));
      },
    );
    return () => {
      active = false;
    };
  }, [state.generation]);

  if (state.data) return <AppShell runtimeConfig={state.data} />;

  return (
    <Box sx={{ display: 'grid', minHeight: '100%', placeItems: 'center', p: 3 }}>
      <Paper variant="outlined" sx={{ width: 'min(440px, 100%)', p: 3 }}>
        <Stack spacing={1.5} sx={{ alignItems: 'flex-start' }}>
          <Typography component="h1" variant="h2">
            {state.error ? 'Unable to initialize Lum Harbor' : 'Starting Lum Harbor…'}
          </Typography>
          {state.error && (
            <>
              <Typography role="alert" variant="body2" color="text.secondary">
                Runtime configuration could not be loaded: {state.error}
              </Typography>
              <Button
                size="small"
                variant="outlined"
                onClick={() =>
                  setState((current) => ({
                    data: null,
                    error: null,
                    generation: current.generation + 1,
                  }))
                }
              >
                Retry initialization
              </Button>
            </>
          )}
        </Stack>
      </Paper>
    </Box>
  );
}

export default App;
