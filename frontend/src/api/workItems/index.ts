import type { WorkItemApi } from './contracts';
import { MockWorkItemApi } from './mockWorkItemApi';

export const workItemApi: WorkItemApi = new MockWorkItemApi();
export type * from './contracts';
