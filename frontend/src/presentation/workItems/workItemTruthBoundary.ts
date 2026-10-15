import type { ProductWorkItem } from '../../api/workItems/productApi.ts';
import {
  buildWorkflowModelFromWorkItem,
  type WorkflowModel,
} from '../../components/workItems/workflow/workflowTypes.ts';

export type WorkflowPresentationOwnership =
  'FUTURE_SUBSYSTEMS_UNAVAILABLE' | 'PROTOTYPE_FUTURE_SUBSYSTEMS';

export interface WorkItemSurfaceModel {
  authoritative: ProductWorkItem;
  workflow: WorkflowModel;
  workflowOwnership: WorkflowPresentationOwnership;
}

/**
 * Joins the authoritative WorkItem shell to optional frontend-only workflow
 * presentation data. The WorkItem always owns identity, title, and
 * description, so a prototype can never overwrite those visible facts.
 * Lifecycle and timestamps remain available only on `authoritative`.
 */
export function buildWorkItemSurfaceModel(
  authoritative: ProductWorkItem,
  prototypeWorkflow?: WorkflowModel,
): WorkItemSurfaceModel {
  const workflow = prototypeWorkflow ?? buildWorkflowModelFromWorkItem(authoritative);
  return {
    authoritative,
    workflow: {
      ...workflow,
      workItemId: authoritative.id,
      title: authoritative.title,
      description: authoritative.description,
      workflowDataSource: prototypeWorkflow ? 'prototype' : 'unavailable',
    },
    workflowOwnership: prototypeWorkflow
      ? 'PROTOTYPE_FUTURE_SUBSYSTEMS'
      : 'FUTURE_SUBSYSTEMS_UNAVAILABLE',
  };
}
