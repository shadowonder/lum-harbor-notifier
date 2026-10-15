import type { HistoryApi } from './contracts';
import { wailsHistoryApi } from './wailsHistoryApi';

export const historyApi: HistoryApi = wailsHistoryApi;
export type * from './contracts';
