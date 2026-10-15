import {
  CreateWorkItem,
  GetWorkItemOverview,
  GetWorkItems,
  SetWorkItemLifecycleState,
} from '../../../wailsjs/go/app/App';
import type { CreateProductWorkItemInput, ProductWorkItemApi } from './productApi';
import { mapWorkItemDTO, mapWorkItemOverviewDTO } from './workItemDtoAdapter.ts';

export const productWorkItemApi: ProductWorkItemApi = {
  async list() {
    return (await GetWorkItems()).map(mapWorkItemDTO);
  },
  async overview() {
    return mapWorkItemOverviewDTO(await GetWorkItemOverview());
  },
  async create(input: CreateProductWorkItemInput) {
    return mapWorkItemDTO(
      await CreateWorkItem({
        title: input.title,
        ...(input.description ? { description: input.description } : {}),
      }),
    );
  },
  async setLifecycle(input) {
    return mapWorkItemDTO(
      await SetWorkItemLifecycleState({
        id: input.id,
        targetLifecycleState: input.targetLifecycleState,
      }),
    );
  },
};
