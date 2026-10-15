import {
  GetCausalChain,
  GetCausalChainBySubject,
  GetHistoryTrace,
  ListHistoryTraces,
  QueryHistory,
  QueryWorkItemHistory,
} from '../../../wailsjs/go/app/App';
import { mapCausalChain, mapHistoryEvent, mapTrace, mapTraceSummary } from './historyDtoAdapter';
import type { HistoryApi } from './contracts';

export const wailsHistoryApi: HistoryApi = {
  async listEvents(query = { days: 30 }) {
    const dto = await QueryHistory({
      days: query.days,
      scope: query.scope ?? '',
      search: query.search ?? '',
    });
    return {
      asOf: dto.asOf,
      events: (dto.events ?? []).map(mapHistoryEvent),
      truncated: dto.truncated,
    };
  },
  async listTraces() {
    const dto = await ListHistoryTraces();
    return { traces: (dto.traces ?? []).map(mapTraceSummary), truncated: dto.truncated };
  },
  async listWorkItemEvents(workItemId) {
    const dto = await QueryWorkItemHistory(workItemId);
    return {
      asOf: dto.asOf,
      events: (dto.events ?? []).map(mapHistoryEvent),
      truncated: dto.truncated,
    };
  },
  async getTrace(traceId) {
    return mapTrace(await GetHistoryTrace(traceId));
  },
  async getCausalChain(targetEventId) {
    return mapCausalChain(await GetCausalChain(targetEventId));
  },
  async getCausalChainBySubject(subjectEntityId) {
    return mapCausalChain(await GetCausalChainBySubject(subjectEntityId));
  },
};
