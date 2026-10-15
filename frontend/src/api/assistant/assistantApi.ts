/**
 * Transport-free Assistant contract. The UI may say only which surface and
 * which WorkItem is selected; WorkItem facts are re-read by the backend and
 * never travel from React.
 */
export type AssistantResultKind = 'ANSWER' | 'MUTATION_INTENT' | 'NEEDS_CONTEXT' | 'UNSUPPORTED';

export type AssistantSurface =
  | 'WORK_ITEMS'
  | 'PLANNER'
  | 'DASHBOARD'
  | 'NOTIFICATIONS'
  | 'HISTORY'
  | 'SETTINGS'
  | '';

export interface AssistantContextLocator {
  surface: AssistantSurface;
  workItemId: string;
}

/**
 * What happened to a MUTATION_INTENT. `CREATED` means a real durable PENDING
 * Proposal exists (proposalId); it never means a WorkItem changed.
 */
export type AssistantProposalOutcome = '' | 'CREATED' | 'NO_CHANGE' | 'CONTEXT_CHANGED';

export interface AssistantResult {
  kind: AssistantResultKind;
  text: string;
  contextLabel: string;
  changeApplied: boolean;
  proposalOutcome: AssistantProposalOutcome;
  proposalId: string;
}

export interface AssistantApi {
  ask(message: string, context: AssistantContextLocator): Promise<AssistantResult>;
}

const kinds: readonly AssistantResultKind[] = [
  'ANSWER',
  'MUTATION_INTENT',
  'NEEDS_CONTEXT',
  'UNSUPPORTED',
];

const proposalOutcomes: readonly AssistantProposalOutcome[] = [
  '',
  'CREATED',
  'NO_CHANGE',
  'CONTEXT_CHANGED',
];

export function mapAssistantResult(dto: {
  kind: string;
  text: string;
  contextLabel: string;
  changeApplied: boolean;
  proposalOutcome?: string;
  proposalId?: string;
}): AssistantResult {
  if (!kinds.includes(dto.kind as AssistantResultKind)) {
    throw new Error(`Unsupported Assistant result kind: ${dto.kind}`);
  }
  const outcome = dto.proposalOutcome ?? '';
  if (!proposalOutcomes.includes(outcome as AssistantProposalOutcome)) {
    throw new Error(`Unsupported Assistant proposal outcome: ${outcome}`);
  }
  const proposalId = dto.proposalId ?? '';
  if (outcome === 'CREATED' && proposalId.trim() === '') {
    throw new Error('Assistant reported a created Proposal without a proposal ID');
  }
  return {
    kind: dto.kind as AssistantResultKind,
    text: dto.text,
    contextLabel: dto.contextLabel,
    changeApplied: dto.changeApplied,
    proposalOutcome: outcome as AssistantProposalOutcome,
    proposalId,
  };
}
