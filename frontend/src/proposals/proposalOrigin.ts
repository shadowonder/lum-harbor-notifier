import type { ProductProposal } from '../api/proposals/proposalProductApi.ts';

/** Where a Proposal's evidence entered Lum Harbor, shown as the review provenance chip. */
export type ProposalOrigin = 'assistant' | 'source';

/**
 * Assistant turns are real Sources (core.assistant). All other linked Source
 * revisions use the universal Source label; producer identity remains provenance.
 */
export function proposalOrigin(
  proposal: Pick<ProductProposal, 'aiRunId' | 'sourceRevisionIds' | 'sourceExtensionIds'>,
): ProposalOrigin | null {
  const extensions = proposal.sourceExtensionIds ?? [];
  if (extensions.includes('core.assistant')) return 'assistant';
  if ((proposal.sourceRevisionIds?.length ?? 0) > 0) return 'source';
  return proposal.aiRunId ? 'assistant' : null;
}

export function proposalOriginLabel(origin: ProposalOrigin): string {
  switch (origin) {
    case 'assistant':
      return 'AI Assistant';
    case 'source':
      return 'From Source';
  }
}

/** Label for the compact Needs Review row, which marks only Source-derived (non-Assistant) Proposals. */
export function sourceOriginRowLabel(
  proposal: Pick<ProductProposal, 'aiRunId' | 'sourceRevisionIds' | 'sourceExtensionIds'>,
): string | null {
  const origin = proposalOrigin(proposal);
  return origin && origin !== 'assistant' ? proposalOriginLabel(origin) : null;
}

/**
 * Detailed provenance label for Proposal Review header.
 * Formats as "From Source · Synthetic Project Source (workflow-002/story-005)"
 * when provenance information is available.
 */
export function proposalProvenanceLabel(
  proposal: Pick<
    ProductProposal,
    'aiRunId' | 'sourceRevisionIds' | 'sourceExtensionIds' | 'sourceProvenances'
  >,
): string {
  const origin = proposalOrigin(proposal);
  if (!origin) return '';
  if (origin === 'assistant') return proposalOriginLabel(origin);

  const base = proposalOriginLabel(origin);
  const provenances = proposal.sourceProvenances ?? [];
  if (provenances.length > 0) {
    const p = provenances[0];
    const name = p.extensionName || p.extensionId;
    if (name && name !== 'Source') {
      const details = p.externalId ? `${name} (${p.externalId})` : name;
      return `${base} · ${details}`;
    }
  }
  return base;
}
