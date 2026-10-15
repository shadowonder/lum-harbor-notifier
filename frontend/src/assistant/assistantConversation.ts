import type {
  AssistantApi,
  AssistantContextLocator,
  AssistantProposalOutcome,
  AssistantResultKind,
  AssistantSurface,
} from '../api/assistant/assistantApi.ts';

/**
 * Result of handing a created Proposal to the existing M9A review flow.
 * STARTED: review began from the refreshed authoritative collection.
 * MISSING: refresh succeeded but the Proposal is not in it (reconciliation).
 * SKIPPED: the user closed the Assistant while the request was in flight.
 */
export type ProposalHandoffResult = 'STARTED' | 'MISSING' | 'SKIPPED';

export type AssistantEntry =
  | { id: string; role: 'user'; text: string }
  | {
      id: string;
      role: 'assistant';
      kind: AssistantResultKind;
      text: string;
      contextLabel: string;
      changeApplied: boolean;
      proposalOutcome: AssistantProposalOutcome;
      proposalId: string;
    }
  | { id: string; role: 'error'; text: string };

/** Builds the surface- and selection-aware context locator. */
export function locatorForContext(
  surface: AssistantSurface | undefined,
  workItemId: string | null | undefined,
): AssistantContextLocator {
  return {
    surface: surface || '',
    workItemId: workItemId ?? '',
  };
}

/** Builds the page-aware locator. Backward compatible with workItemId-only callers. */
export function locatorForWorkItem(
  workItemId: string | null,
  surfaceHint?: AssistantSurface,
): AssistantContextLocator {
  if (workItemId) {
    return { surface: surfaceHint || 'WORK_ITEMS', workItemId };
  }
  return { surface: surfaceHint || '', workItemId: '' };
}

let sequence = 0;
const nextId = (prefix: string) => `${prefix}-${++sequence}`;

/**
 * Sends one message and returns the transcript entries it produced. A backend
 * failure becomes an honest error entry; there is never a demo/fallback answer.
 */
export async function sendAssistantMessage(
  api: AssistantApi,
  message: string,
  workItemId: string | null,
  onProposalCreated?: (proposalId: string) => Promise<ProposalHandoffResult>,
  surfaceHint?: AssistantSurface,
): Promise<AssistantEntry[]> {
  const text = message.trim();
  if (!text) return [];
  const user: AssistantEntry = { id: nextId('user'), role: 'user', text };
  try {
    const locator = locatorForWorkItem(workItemId, surfaceHint);
    const result = await api.ask(text, locator);
    const entries: AssistantEntry[] = [
      user,
      {
        id: nextId('assistant'),
        role: 'assistant',
        kind: result.kind,
        text: result.text,
        contextLabel: result.contextLabel,
        changeApplied: result.changeApplied,
        proposalOutcome: result.proposalOutcome,
        proposalId: result.proposalId,
      },
    ];
    if (result.proposalOutcome === 'CREATED' && onProposalCreated) {
      // The durable Proposal already exists. Nothing below may be reported as
      // a creation failure, and the AI request is never re-sent.
      try {
        const handoff = await onProposalCreated(result.proposalId);
        if (handoff === 'MISSING') {
          entries.push({
            id: nextId('error'),
            role: 'error',
            text: 'Proposal was created, but it is not currently available in the review list.',
          });
        }
      } catch {
        entries.push({
          id: nextId('error'),
          role: 'error',
          text: 'Proposal was created, but the review view could not be loaded.',
        });
      }
    }
    return entries;
  } catch (error) {
    return [
      user,
      {
        id: nextId('error'),
        role: 'error',
        text: error instanceof Error ? error.message : String(error),
      },
    ];
  }
}

export function assistantKindLabel(
  kind: AssistantResultKind,
  proposalOutcome: AssistantProposalOutcome = '',
): string | null {
  if (kind === 'MUTATION_INTENT') {
    if (proposalOutcome === 'CREATED') return 'Proposal created for review · nothing was changed';
    if (proposalOutcome === 'NO_CHANGE') return 'No change produced · nothing was changed';
    if (proposalOutcome === 'CONTEXT_CHANGED') return 'Work Item changed · no Proposal created';
    return 'Change request recognized · nothing was changed';
  }
  if (kind === 'NEEDS_CONTEXT') return 'Needs context';
  if (kind === 'UNSUPPORTED') return 'Not supported';
  return null;
}
