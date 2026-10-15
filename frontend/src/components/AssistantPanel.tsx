import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import AutoAwesomeRounded from '@mui/icons-material/AutoAwesomeRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import SendRounded from '@mui/icons-material/SendRounded';
import { useMemo, type KeyboardEvent, type RefObject } from 'react';
import {
  Alert,
  Box,
  Button,
  ButtonBase,
  Chip,
  Divider,
  IconButton,
  InputAdornment,
  Paper,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { isAggregateProposal, type ProductProposal } from '../api/proposals/proposalProductApi.ts';
import { proposalReviewRowHint } from '../proposals/aggregateProposalReview.ts';
import AggregateProposalReview from './proposals/AggregateProposalReview';
import { assistantKindLabel, type AssistantEntry } from '../assistant/assistantConversation.ts';
import {
  proposalOrigin,
  proposalOriginLabel,
  proposalProvenanceLabel,
  sourceOriginRowLabel,
} from '../proposals/proposalOrigin.ts';

interface AssistantPanelProps {
  context: string;
  input: string;
  inputRef: RefObject<HTMLInputElement | null>;
  proposals: ProductProposal[];
  proposalsLoading: boolean;
  proposalsError: string | null;
  activeProposal: ProductProposal | null;
  proposalMutation: 'apply' | 'reject' | null;
  proposalActionError: string | null;
  proposalActionNotice: {
    severity: 'success' | 'info' | 'warning';
    message: string;
  } | null;
  entries: AssistantEntry[];
  submitting: boolean;
  onSend: (message: string) => Promise<void>;
  onClose: () => void;
  onInputChange: (value: string) => void;
  onBeginReview: (proposal: ProductProposal) => void;
  onEndReview: () => void;
  onApplyProposal: () => Promise<void>;
  onRejectProposal: () => Promise<void>;
  onRetryProposals: () => void;
}

const suggestions = ['Summarize this work', 'What is this Work Item about?'];

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{
        display: 'block',
        mb: 0.8,
        fontWeight: 720,
        letterSpacing: '.08em',
        textTransform: 'uppercase',
      }}
    >
      {children}
    </Typography>
  );
}

function FieldChange({ label, before, after }: { label: string; before: string; after: string }) {
  return (
    <Box sx={{ display: 'grid', gridTemplateColumns: '82px minmax(0, 1fr)', gap: 1 }}>
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Box sx={{ minWidth: 0 }}>
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ overflowWrap: 'anywhere', textDecoration: 'line-through' }}
        >
          {before || 'Not set'}
        </Typography>
        <Typography variant="body2" sx={{ mt: 0.25, fontWeight: 620, overflowWrap: 'anywhere' }}>
          {after || 'Not set'}
        </Typography>
      </Box>
    </Box>
  );
}

function ProposalReview({
  proposal,
  mutation,
  error,
  onBack,
  onApply,
  onReject,
}: {
  proposal: ProductProposal;
  mutation: 'apply' | 'reject' | null;
  error: string | null;
  onBack: () => void;
  onApply: () => Promise<void>;
  onReject: () => Promise<void>;
}) {
  if (isAggregateProposal(proposal)) {
    return (
      <AggregateProposalReview
        proposal={proposal}
        mutation={mutation}
        error={error}
        onBack={onBack}
        onApply={onApply}
        onReject={onReject}
      />
    );
  }
  const why = proposal.reason || proposal.summary || 'No additional rationale was provided.';
  const origin = proposalOrigin(proposal);
  return (
    <Box sx={{ display: 'flex', flex: 1, minHeight: 0, flexDirection: 'column' }}>
      <Box sx={{ flex: 1, minHeight: 0, overflowY: 'auto', px: 2.5, py: 1.75 }}>
        <Button
          size="small"
          color="inherit"
          startIcon={<ArrowBackRounded />}
          onClick={onBack}
          disabled={mutation !== null}
          sx={{ ml: -1, mb: 1, color: 'text.secondary' }}
        >
          Suggested Changes
        </Button>
        <Stack
          direction="row"
          spacing={1}
          useFlexGap
          sx={{ alignItems: 'center', flexWrap: 'wrap' }}
        >
          <Typography
            component="h3"
            sx={{ fontSize: 17, fontWeight: 680, letterSpacing: '-.02em' }}
          >
            {proposal.title}
          </Typography>
          <Chip
            size="small"
            label="Previewing"
            variant="outlined"
            sx={{
              height: 21,
              color: '#7969C7',
              borderStyle: 'dashed',
              borderColor: 'currentColor',
            }}
          />
          {origin && origin !== 'assistant' && (
            <Chip
              size="small"
              label={proposalProvenanceLabel(proposal)}
              variant="outlined"
              color="info"
              sx={{ height: 21, fontSize: '0.75rem' }}
              data-testid="proposal-source-provenance-badge"
            />
          )}
          {origin === 'assistant' && (
            <Chip
              size="small"
              label={proposalOriginLabel(origin)}
              variant="outlined"
              color="secondary"
              sx={{ height: 21, fontSize: '0.75rem' }}
              data-testid="proposal-assistant-provenance-badge"
            />
          )}
        </Stack>
        {proposal.summary && (
          <Typography variant="body2" color="text.secondary" sx={{ display: 'block', mt: 0.75 }}>
            {proposal.summary}
          </Typography>
        )}

        <Box sx={{ mt: 2.2 }}>
          <SectionLabel>Why</SectionLabel>
          <Typography variant="body2" color="text.secondary">
            {why}
          </Typography>
        </Box>
        <Divider sx={{ my: 2 }} />
        <SectionLabel>Proposed Changes</SectionLabel>
        <Stack spacing={1.4}>
          {(proposal.changes?.length
            ? proposal.changes
            : [
                { label: 'Title', before: proposal.baseTitle, after: proposal.proposedTitle },
                {
                  label: 'Description',
                  before: proposal.baseDescription,
                  after: proposal.proposedDescription,
                },
              ]
          ).map((change) => (
            <FieldChange
              key={change.label}
              label={change.label}
              before={change.before ?? ''}
              after={change.after ?? ''}
            />
          ))}
        </Stack>
        <Divider sx={{ my: 2 }} />
        <SectionLabel>Impact</SectionLabel>
        <Typography variant="body2" color="text.secondary">
          Applying uses the authoritative domain service in one transaction. Preview and Reject do
          not mutate current truth.
        </Typography>
      </Box>
      <Box
        sx={{
          flexShrink: 0,
          p: 2,
          borderTop: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        {error && (
          <Alert severity="error" sx={{ mb: 1.25 }}>
            Proposal action failed: {error}. Review remains active so you can retry.
          </Alert>
        )}
        <Stack direction="row" spacing={1} sx={{ justifyContent: 'space-between' }}>
          <Button
            color="inherit"
            variant="outlined"
            disabled={mutation !== null}
            onClick={() => void onReject()}
          >
            {mutation === 'reject' ? 'Rejecting…' : 'Reject'}
          </Button>
          <Button variant="contained" disabled={mutation !== null} onClick={() => void onApply()}>
            {mutation === 'apply' ? 'Applying…' : 'Apply'}
          </Button>
        </Stack>
      </Box>
    </Box>
  );
}

export default function AssistantPanel(props: AssistantPanelProps) {
  const pending = useMemo(
    () =>
      props.proposals
        .filter((proposal) => proposal.status === 'PENDING')
        .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()),
    [props.proposals],
  );

  const selectSuggestion = (suggestion: string) => {
    props.onInputChange(suggestion);
    requestAnimationFrame(() => props.inputRef.current?.focus());
  };
  const submit = () => {
    const request = props.input.trim();
    if (!request || props.submitting) return;
    props.onInputChange('');
    void props.onSend(request);
  };
  const handleInputKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
  };

  return (
    <Box
      component="aside"
      aria-label="AI Assistant"
      sx={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        height: '100%',
        bgcolor: 'background.paper',
      }}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{
          height: 60,
          px: 2.5,
          flexShrink: 0,
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <AutoAwesomeRounded sx={{ fontSize: 18, color: 'primary.main' }} />
          <Typography component="h2" variant="h2">
            AI Assistant
          </Typography>
        </Stack>
        <Tooltip title="Close assistant (Esc)">
          <IconButton size="small" onClick={props.onClose} aria-label="Close AI Assistant">
            <CloseRounded fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      <Divider />
      <Box sx={{ px: 2.5, py: 1.3, flexShrink: 0 }}>
        <SectionLabel>Context</SectionLabel>
        <Chip label={props.context} size="small" color="primary" variant="outlined" />
      </Box>
      <Divider />

      {props.activeProposal ? (
        <ProposalReview
          proposal={props.activeProposal}
          mutation={props.proposalMutation}
          error={props.proposalActionError}
          onBack={props.onEndReview}
          onApply={props.onApplyProposal}
          onReject={props.onRejectProposal}
        />
      ) : (
        <>
          <Box sx={{ flex: 1, minHeight: 0, overflowY: 'auto', px: 2.5, py: 2 }}>
            <Stack
              direction="row"
              sx={{ mb: 1, alignItems: 'center', justifyContent: 'space-between' }}
            >
              <SectionLabel>Needs Review</SectionLabel>
              <Chip label={pending.length} size="small" sx={{ height: 20 }} />
            </Stack>
            {props.proposalActionNotice && (
              <Alert severity={props.proposalActionNotice.severity} sx={{ mb: 1.25 }}>
                {props.proposalActionNotice.message}
              </Alert>
            )}
            {props.proposalsLoading && (
              <Typography role="status" variant="body2" color="text.secondary">
                Loading suggestions…
              </Typography>
            )}
            {props.proposalsError && (
              <Alert
                severity="error"
                action={
                  <Button size="small" color="inherit" onClick={props.onRetryProposals}>
                    Retry
                  </Button>
                }
              >
                Suggested changes could not be loaded: {props.proposalsError}
              </Alert>
            )}
            {!props.proposalsLoading && !props.proposalsError && pending.length === 0 && (
              <Typography variant="body2" color="text.secondary">
                No pending suggestions.
              </Typography>
            )}
            <Stack spacing={0.7}>
              {pending.map((proposal) => (
                <ButtonBase
                  key={proposal.id}
                  onClick={() => props.onBeginReview(proposal)}
                  sx={{
                    width: '100%',
                    p: 1.1,
                    border: 1,
                    borderColor: 'divider',
                    borderRadius: 1,
                    textAlign: 'left',
                    alignItems: 'flex-start',
                    '&:hover': { bgcolor: 'surface.subtle', borderColor: 'primary.main' },
                    '&:focus-visible': {
                      outline: '2px solid',
                      outlineColor: 'primary.main',
                      outlineOffset: 1,
                    },
                  }}
                >
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography variant="body2" sx={{ fontWeight: 680 }}>
                      {proposal.title}
                    </Typography>
                    {sourceOriginRowLabel(proposal) && (
                      <Chip
                        size="small"
                        label={sourceOriginRowLabel(proposal)}
                        variant="outlined"
                        color="info"
                        sx={{ height: 18, fontSize: '0.68rem', mt: 0.3 }}
                        data-testid="proposal-item-source-badge"
                      />
                    )}
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ display: 'block', mt: 0.25 }}
                    >
                      {proposal.summary || proposal.reason || 'WorkItem details update'}
                    </Typography>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{ display: 'block', mt: 0.45 }}
                    >
                      {proposalReviewRowHint(proposal)}
                    </Typography>
                  </Box>
                </ButtonBase>
              ))}
            </Stack>
            <Divider sx={{ my: 2.2 }} />
            <SectionLabel>Ask Lum Harbor</SectionLabel>
            <Stack spacing={1} sx={{ mb: 1 }} aria-live="polite">
              {props.entries.map((entry) =>
                entry.role === 'user' ? (
                  <Typography
                    key={entry.id}
                    variant="body2"
                    sx={{ alignSelf: 'flex-end', maxWidth: '92%', color: 'text.secondary' }}
                  >
                    {entry.text}
                  </Typography>
                ) : entry.role === 'error' ? (
                  <Box
                    key={entry.id}
                    role="alert"
                    sx={{
                      p: 1,
                      borderLeft: 2,
                      borderColor: 'error.main',
                      bgcolor: 'surface.subtle',
                    }}
                  >
                    <Typography variant="body2">{entry.text}</Typography>
                  </Box>
                ) : (
                  <Box
                    key={entry.id}
                    sx={{
                      p: 1,
                      borderLeft: 2,
                      borderColor: entry.kind === 'ANSWER' ? 'primary.main' : 'warning.main',
                      bgcolor: 'surface.subtle',
                    }}
                  >
                    {assistantKindLabel(entry.kind, entry.proposalOutcome) && (
                      <Chip
                        size="small"
                        variant="outlined"
                        color="warning"
                        label={assistantKindLabel(entry.kind, entry.proposalOutcome)}
                        sx={{ mb: 0.7, height: 20 }}
                      />
                    )}
                    <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                      {entry.text}
                    </Typography>
                  </Box>
                ),
              )}
              {props.submitting && (
                <Typography variant="caption" color="text.secondary">
                  Thinking…
                </Typography>
              )}
            </Stack>
            <Stack spacing={0.5} sx={{ alignItems: 'flex-start' }}>
              {suggestions.map((suggestion) => (
                <Button
                  key={suggestion}
                  size="small"
                  color="inherit"
                  onClick={() => selectSuggestion(suggestion)}
                  sx={{ px: 0.75, color: 'text.secondary', justifyContent: 'flex-start' }}
                >
                  {suggestion}
                </Button>
              ))}
            </Stack>
          </Box>
          <Box
            sx={{
              flexShrink: 0,
              p: 2,
              borderTop: 1,
              borderColor: 'divider',
              bgcolor: 'surface.subtle',
            }}
          >
            <Paper variant="outlined" sx={{ p: 0.5, borderColor: 'divider', boxShadow: 'none' }}>
              <TextField
                inputRef={props.inputRef}
                value={props.input}
                onChange={(event) => props.onInputChange(event.target.value)}
                onKeyDown={handleInputKeyDown}
                placeholder="Ask Lum Harbor..."
                multiline
                minRows={2}
                maxRows={5}
                fullWidth
                slotProps={{
                  input: {
                    endAdornment: (
                      <InputAdornment position="end" sx={{ alignSelf: 'flex-end', mb: 0.25 }}>
                        <Tooltip title="Send">
                          <span>
                            <IconButton
                              disabled={!props.input.trim() || props.submitting}
                              onClick={submit}
                              size="small"
                              aria-label="Send message"
                            >
                              <SendRounded fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                      </InputAdornment>
                    ),
                    sx: {
                      alignItems: 'flex-start',
                      p: 1,
                      fontSize: 13,
                      '& fieldset': { border: 0 },
                    },
                  },
                }}
              />
            </Paper>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', mt: 0.8, px: 0.5 }}
            >
              The Assistant answers questions. Proposal changes occur only after your explicit Apply
              action.
            </Typography>
          </Box>
        </>
      )}
    </Box>
  );
}
