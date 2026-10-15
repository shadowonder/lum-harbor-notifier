import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import { Alert, Box, Button, Chip, Divider, Stack, Typography } from '@mui/material';
import type {
  ProductProposal,
  ProductProposalOperation,
} from '../../api/proposals/proposalProductApi.ts';
import {
  AGGREGATE_REJECT_LABEL,
  actionNumber,
  aggregateActionCountLabel,
  aggregateApplyLabel,
  aggregateAtomicityNotice,
} from '../../proposals/aggregateProposalReview.ts';
import {
  proposalOrigin,
  proposalOriginLabel,
  proposalProvenanceLabel,
} from '../../proposals/proposalOrigin.ts';

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

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Box sx={{ display: 'grid', gridTemplateColumns: '92px minmax(0, 1fr)', gap: 1 }}>
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Box sx={{ minWidth: 0 }}>{children}</Box>
    </Box>
  );
}

function OperationCard({ operation }: { operation: ProductProposalOperation }) {
  return (
    <Box
      component="li"
      data-testid="aggregate-operation"
      data-sequence-no={operation.sequenceNo}
      aria-label={`Action ${actionNumber(operation.sequenceNo)}: ${operation.label}`}
      sx={{
        listStyle: 'none',
        p: 1.5,
        border: 1,
        borderColor: 'divider',
        borderRadius: 1.5,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 1 }}>
        <Box
          aria-hidden
          sx={{
            flexShrink: 0,
            width: 22,
            height: 22,
            borderRadius: '50%',
            display: 'grid',
            placeItems: 'center',
            fontSize: 12,
            fontWeight: 720,
            color: '#7969C7',
            border: 1,
            borderColor: 'currentColor',
          }}
        >
          {actionNumber(operation.sequenceNo)}
        </Box>
        <Typography component="h4" sx={{ fontSize: 14.5, fontWeight: 680 }}>
          {operation.label}
        </Typography>
      </Stack>
      <Stack spacing={0.9}>
        {operation.relations.map((relation) => (
          <DetailRow key={`relation-${relation.label}`} label={relation.label}>
            <Typography
              variant="body2"
              data-testid="aggregate-operation-relation"
              sx={{ overflowWrap: 'anywhere' }}
            >
              {relation.value}
            </Typography>
          </DetailRow>
        ))}
        {operation.changes.map((change) => (
          <DetailRow key={`change-${change.label}`} label={change.label}>
            {change.before !== null && (
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ overflowWrap: 'anywhere', textDecoration: 'line-through' }}
              >
                {change.before}
              </Typography>
            )}
            <Typography variant="body2" sx={{ fontWeight: 620, overflowWrap: 'anywhere' }}>
              {change.after ?? 'Not set'}
            </Typography>
          </DetailRow>
        ))}
      </Stack>
    </Box>
  );
}

/**
 * Review of an aggregate Proposal: its ordered actions as interpreted by the
 * backend, and one parent-level decision. There is no per-action Apply or
 * Reject; Apply is all-or-nothing.
 */
export default function AggregateProposalReview({
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
  const why = proposal.reason || proposal.summary || 'No additional rationale was provided.';
  const origin = proposalOrigin(proposal);
  const count = proposal.operations.length;
  return (
    <Box
      data-testid="aggregate-proposal-review"
      sx={{ display: 'flex', flex: 1, minHeight: 0, flexDirection: 'column' }}
    >
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
            label={aggregateActionCountLabel(count)}
            variant="outlined"
            data-testid="aggregate-action-count"
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
        <SectionLabel>Proposed Actions</SectionLabel>
        <Stack component="ol" spacing={1.2} sx={{ m: 0, p: 0 }}>
          {proposal.operations.map((operation) => (
            <OperationCard key={operation.id} operation={operation} />
          ))}
        </Stack>
        <Divider sx={{ my: 2 }} />
        <SectionLabel>Impact</SectionLabel>
        <Typography variant="body2" color="text.secondary" data-testid="aggregate-atomicity-notice">
          {aggregateAtomicityNotice(count)}
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
            data-testid="aggregate-reject"
          >
            {mutation === 'reject' ? 'Rejecting…' : AGGREGATE_REJECT_LABEL}
          </Button>
          <Button
            variant="contained"
            disabled={mutation !== null}
            onClick={() => void onApply()}
            data-testid="aggregate-apply"
          >
            {mutation === 'apply' ? 'Applying…' : aggregateApplyLabel(count)}
          </Button>
        </Stack>
      </Box>
    </Box>
  );
}
