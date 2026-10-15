import {
  ApplyProposal,
  GetDashboardProposalSummary,
  GetPendingProposals,
  ProposeWorkItemUpdate,
  RejectProposal,
} from '../../../wailsjs/go/app/App';
import type { ProposalProductApi, ProposeWorkItemUpdateInput } from './proposalProductApi.ts';
import {
  mapApplyProposalResultDTO,
  mapDashboardProposalSummaryDTO,
  mapProposalDTO,
} from './proposalDtoAdapter.ts';

export const wailsProposalProductApi: ProposalProductApi = {
  async getPendingProposals() {
    const list = await GetPendingProposals();
    return list.map(mapProposalDTO);
  },
  async proposeWorkItemUpdate(input: ProposeWorkItemUpdateInput) {
    const targetWorkItemId = input.targetWorkItemId.trim();
    if (!targetWorkItemId) throw new Error('Target Work Item ID is required.');
    const proposedTitle = input.proposedTitle.trim();
    if (!proposedTitle) throw new Error('Proposed title is required.');
    const proposedDescription = input.proposedDescription?.trim();
    const reason = input.reason?.trim();

    const dto = await ProposeWorkItemUpdate({
      targetWorkItemId,
      proposedTitle,
      ...(proposedDescription ? { proposedDescription } : {}),
      ...(reason ? { reason } : {}),
    });
    return mapProposalDTO(dto);
  },
  async applyProposal(id: string) {
    const trimmedId = id.trim();
    if (!trimmedId) throw new Error('Proposal ID is required.');
    const dto = await ApplyProposal({ id: trimmedId });
    return mapApplyProposalResultDTO(dto);
  },
  async rejectProposal(id: string) {
    const trimmedId = id.trim();
    if (!trimmedId) throw new Error('Proposal ID is required.');
    const dto = await RejectProposal({ id: trimmedId });
    return mapProposalDTO(dto);
  },
  async getDashboardSummary() {
    const dto = await GetDashboardProposalSummary();
    return mapDashboardProposalSummaryDTO(dto);
  },
};
