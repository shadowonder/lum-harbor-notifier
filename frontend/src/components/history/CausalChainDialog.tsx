import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  Stack,
  Typography,
} from '@mui/material';
import { useEffect, useState } from 'react';
import { historyApi } from '../../api/history';
import type { HistoryCausalChainDto } from '../../api/history/contracts';
import { rootSourceLabel, rootSourceText } from './rootSourceText';

interface CausalChainDialogProps {
  eventId: string | null;
  onClose: () => void;
}

const dateTimeFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
  second: '2-digit',
});

export function CausalChainDialog({ eventId, onClose }: CausalChainDialogProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [chain, setChain] = useState<HistoryCausalChainDto | null>(null);

  useEffect(() => {
    if (!eventId) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- reset state when dialog closes
      setChain(null);
      setError(null);
      return;
    }
    let active = true;
    setLoading(true);
    setError(null);

    if (historyApi.getCausalChain) {
      historyApi
        .getCausalChain(eventId)
        .then((result) => {
          if (active) {
            setChain(result);
          }
        })
        .catch((err) => {
          if (active) {
            setError(err?.message ?? 'Failed to load causal chain');
          }
        })
        .finally(() => {
          if (active) {
            setLoading(false);
          }
        });
    } else {
      setError('Causal chain API is unavailable');
      setLoading(false);
    }

    return () => {
      active = false;
    };
  }, [eventId]);

  return (
    <Dialog open={Boolean(eventId)} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle
        sx={{ m: 0, p: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}
      >
        <Box>
          <Typography variant="h6" component="div">
            Causal Lineage &amp; Provenance
          </Typography>
          <Typography variant="caption" color="text.secondary">
            Traverse upstream why it happened and downstream what it caused
          </Typography>
        </Box>
        <IconButton aria-label="close" onClick={onClose} size="small">
          <CloseRounded fontSize="small" />
        </IconButton>
      </DialogTitle>
      <Divider />
      <DialogContent sx={{ p: 2.5 }}>
        {loading && (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 5 }}>
            <CircularProgress size={32} />
          </Box>
        )}
        {error && (
          <Box sx={{ py: 3, textAlign: 'center' }}>
            <Typography color="error.main">{error}</Typography>
          </Box>
        )}
        {!loading && !error && chain && (
          <Box>
            {chain.rootEvent && (
              <Box
                sx={{
                  mb: 2.5,
                  p: 1.5,
                  bgcolor: 'surface.secondary',
                  borderRadius: 1.5,
                  border: 1,
                  borderColor: 'divider',
                }}
              >
                <Typography
                  variant="caption"
                  sx={{
                    fontWeight: 700,
                    letterSpacing: '0.06em',
                    textTransform: 'uppercase',
                    color: 'primary.main',
                  }}
                >
                  Root Cause / Origin
                </Typography>
                <Typography variant="body2" sx={{ fontWeight: 650, mt: 0.25 }}>
                  {chain.rootEvent.title}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                  {chain.rootEvent.actor.label} ({chain.rootEvent.actor.kind}) ·{' '}
                  {dateTimeFormatter.format(new Date(chain.rootEvent.occurredAt))}
                </Typography>
                {chain.rootEvent.summary && (
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                    {chain.rootEvent.summary}
                  </Typography>
                )}
                {chain.rootSource && (
                  <Box
                    sx={{ mt: 1.25, pt: 1, borderTop: 1, borderColor: 'divider' }}
                    data-testid="causal-root-source"
                  >
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                      Original input · {rootSourceLabel(chain.rootSource)}
                    </Typography>
                    <Typography
                      variant="body2"
                      sx={{ mt: 0.25, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
                    >
                      {rootSourceText(chain.rootSource)}
                    </Typography>
                  </Box>
                )}
              </Box>
            )}

            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                fontWeight: 700,
                letterSpacing: '0.08em',
                textTransform: 'uppercase',
                display: 'block',
                mb: 1.5,
              }}
            >
              Chronological Causal Chain ({chain.events.length} events)
            </Typography>

            <Box sx={{ pl: 1 }}>
              {chain.events.map((event, index) => {
                const isTarget = event.id === chain.targetEventId;
                const isLast = index === chain.events.length - 1;
                return (
                  <Box
                    key={event.id}
                    sx={{
                      display: 'grid',
                      gridTemplateColumns: '24px minmax(0, 1fr)',
                      columnGap: 1.5,
                      position: 'relative',
                    }}
                  >
                    <Box sx={{ position: 'relative', display: 'flex', justifyContent: 'center' }}>
                      {!isLast && (
                        <Box
                          aria-hidden="true"
                          sx={{
                            position: 'absolute',
                            top: 20,
                            bottom: -8,
                            width: 2,
                            bgcolor: isTarget ? 'primary.main' : 'divider',
                          }}
                        />
                      )}
                      <Box
                        sx={{
                          zIndex: 1,
                          width: 18,
                          height: 18,
                          display: 'grid',
                          placeItems: 'center',
                          borderRadius: '50%',
                          border: 2,
                          borderColor: isTarget ? 'primary.main' : 'divider',
                          bgcolor: isTarget ? 'primary.main' : 'background.paper',
                          color: isTarget ? 'primary.contrastText' : 'text.secondary',
                        }}
                      >
                        <ChevronRightRounded sx={{ fontSize: 13 }} />
                      </Box>
                    </Box>

                    <Box sx={{ pb: 2 }}>
                      <Stack
                        direction="row"
                        spacing={1}
                        sx={{ alignItems: 'baseline', justifyContent: 'space-between' }}
                      >
                        <Typography
                          variant="caption"
                          sx={{
                            fontWeight: 700,
                            letterSpacing: '0.06em',
                            textTransform: 'uppercase',
                            color: isTarget ? 'primary.main' : 'text.secondary',
                          }}
                        >
                          {event.eventType} {isTarget ? '★ TARGET EVENT' : ''}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {dateTimeFormatter.format(new Date(event.occurredAt))}
                        </Typography>
                      </Stack>
                      <Typography sx={{ fontWeight: isTarget ? 700 : 600, mt: 0.2 }}>
                        {event.title}
                      </Typography>
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.25 }}>
                        {event.summary}
                      </Typography>
                      {event.source && (
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          sx={{ display: 'block', mt: 0.25 }}
                        >
                          {event.source}
                        </Typography>
                      )}
                      {event.causationId && (
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          sx={{ display: 'block', mt: 0.2 }}
                        >
                          Direct cause event: {event.causationId}
                        </Typography>
                      )}
                    </Box>
                  </Box>
                );
              })}
            </Box>
          </Box>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 2.5, py: 1.5 }}>
        <Button onClick={onClose} variant="outlined" size="small">
          Close
        </Button>
      </DialogActions>
    </Dialog>
  );
}
