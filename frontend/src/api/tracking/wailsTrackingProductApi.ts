import {
  CreateMonitorPoint,
  CreateTrackedItem,
  CreateWorkflow,
  CreateWorkflowDefinition,
  GetWorkItemTracking,
  ListActiveWorkflowDefinitions,
  SetMonitorPointState,
  SetTrackedItemStepState,
} from '../../../wailsjs/go/app/App';
import { app } from '../../../wailsjs/go/models';
import {
  mapMonitorPoint,
  mapStepState,
  mapTrackedItem,
  mapWorkflow,
  mapWorkflowDefinition,
  mapWorkItemTracking,
  normalizeTrackingError,
} from './trackingDtoAdapter.ts';
import type { ProductTrackingApi } from './trackingProductApi.ts';

async function call<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    throw normalizeTrackingError(error);
  }
}

export const productTrackingApi: ProductTrackingApi = {
  async getWorkItemTracking(workItemId) {
    return mapWorkItemTracking(await call(() => GetWorkItemTracking(workItemId)));
  },
  async listActiveWorkflows() {
    return (await call(() => ListActiveWorkflowDefinitions())).map(mapWorkflowDefinition);
  },
  async createWorkflow(input) {
    return mapWorkflowDefinition(
      await call(() =>
        CreateWorkflowDefinition(
          app.CreateWorkflowDefinitionRequestDTO.createFrom({
            name: input.name,
            version: input.version,
            steps: input.steps,
          }),
        ),
      ),
    );
  },
  async createWorkflowForWorkItem(input) {
    return mapWorkflow(
      await call(() =>
        CreateWorkflow({
          workItemId: input.workItemId,
          workflowDefinitionId: input.workflowDefinitionId,
        }),
      ),
    );
  },
  async createTrackedItem(input) {
    return mapTrackedItem(
      await call(() =>
        CreateTrackedItem({
          workItemId: input.workItemId,
          title: input.title,
          itemType: input.itemType,
          parentItemId: input.parentItemId,
          workflowId: input.workflowId,
        }),
      ),
    );
  },
  async setStepState(input) {
    return mapStepState(
      await call(() =>
        SetTrackedItemStepState({
          trackedItemId: input.trackedItemId,
          workflowStepId: input.workflowStepId,
          state: input.state,
        }),
      ),
    );
  },
  async createMonitor(input) {
    return mapMonitorPoint(
      await call(() =>
        CreateMonitorPoint({
          workItemId: input.workItemId,
          kind: input.kind,
          title: input.title,
          dueAt: input.dueAt,
          timezone: input.timezone,
          expectedWorkItemLifecycle: input.expectedWorkItemLifecycle,
        }),
      ),
    );
  },
  async setMonitorState(input) {
    return mapMonitorPoint(
      await call(() => SetMonitorPointState({ id: input.id, targetState: input.target })),
    );
  },
};
