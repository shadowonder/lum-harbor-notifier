import type {
  TrackedItem,
  TrackingStep,
  TrackingStepState,
  TrackingWorkflow,
  WorkItemTracking,
} from '../api/tracking/trackingProductApi.ts';

/**
 * Pure structure model for the single-workflow detail view.
 *
 * A WorkItem owns at most one Workflow (0..1).
 * All TrackedItems in the WorkItem belong to this single workflow and are
 * mapped onto its ordered stage axis.
 * Independent item states on the same stage are preserved.
 * Hierarchy / parent-child tree connectors are drawn directly between items.
 */

export interface StructureNode {
  item: TrackedItem;
  /** Vertical order inside the workflow (tree order, parents before children). */
  row: number;
  /** Index of the real WorkflowStep the backend reports as current/terminal. */
  column: number;
  /** Backend current step; null only when no step or no workflow. */
  step: TrackingStep | null;
  /** Backend current step state; a complete workflow reads COMPLETED. */
  state: TrackingStepState | null;
  /** 0 for a root item, increasing for descendants. */
  depth: number;
  /** Parent item ID (drawn as a connector), else null. */
  parentId: string | null;
}

export interface WorkflowStructure {
  workflow: TrackingWorkflow | null;
  steps: TrackingStep[];
  nodes: StructureNode[];
}

export function buildWorkflowStructure(tracking: WorkItemTracking): WorkflowStructure {
  const workflow = tracking.workflow;
  if (!workflow) {
    return {
      workflow: null,
      steps: [],
      nodes: [],
    };
  }

  const steps = [...workflow.steps].sort((a, b) => a.sequence - b.sequence);
  const inWorkflow = new Set(tracking.trackedItems.map((item) => item.id));

  // Tree order: a root is an item whose parent is not in this WorkItem's items.
  const children = new Map<string, TrackedItem[]>();
  const roots: TrackedItem[] = [];
  for (const item of tracking.trackedItems) {
    if (item.parentItemId && inWorkflow.has(item.parentItemId)) {
      const list = children.get(item.parentItemId) ?? [];
      list.push(item);
      children.set(item.parentItemId, list);
    } else {
      roots.push(item);
    }
  }

  const nodes: StructureNode[] = [];
  const visit = (item: TrackedItem, depth: number) => {
    nodes.push(nodeFor(item, steps, nodes.length, depth, inWorkflow));
    for (const child of children.get(item.id) ?? []) {
      visit(child, depth + 1);
    }
  };
  roots.forEach((root) => visit(root, 0));

  return { workflow, steps, nodes };
}

function nodeFor(
  item: TrackedItem,
  steps: TrackingStep[],
  row: number,
  depth: number,
  inWorkflow: Set<string>,
): StructureNode {
  const progression = item.progression;
  let step: TrackingStep | null = null;
  let state: TrackingStepState | null = null;
  if (progression?.complete) {
    step = steps[steps.length - 1] ?? null;
    state = 'COMPLETED';
  } else if (progression?.currentStepId) {
    step = steps.find((s) => s.id === progression.currentStepId) ?? null;
    state = progression.currentState;
  }
  const parentInWorkflow = item.parentItemId && inWorkflow.has(item.parentItemId);
  return {
    item,
    row,
    column: step ? Math.max(0, steps.indexOf(step)) : 0,
    step,
    state,
    depth,
    parentId: parentInWorkflow ? item.parentItemId : null,
  };
}

/**
 * Selection focus over the real parent chain (ancestors) and every descendant.
 * Pure hierarchy: no dependency semantics.
 */
export function focusSet(tracking: WorkItemTracking, selectedId: string): Set<string> {
  const byId = new Map(tracking.trackedItems.map((item) => [item.id, item]));
  const focus = new Set<string>([selectedId]);
  let cursor = byId.get(selectedId)?.parentItemId ?? null;
  while (cursor && !focus.has(cursor)) {
    focus.add(cursor);
    cursor = byId.get(cursor)?.parentItemId ?? null;
  }
  const descendants = new Set<string>([selectedId]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const item of tracking.trackedItems) {
      if (item.parentItemId && descendants.has(item.parentItemId) && !descendants.has(item.id)) {
        descendants.add(item.id);
        focus.add(item.id);
        grew = true;
      }
    }
  }
  // Dependency focus is recursive in both directions so the selected item's
  // prerequisite and blocking chains remain legible together.
  let dependencyGrew = true;
  while (dependencyGrew) {
    dependencyGrew = false;
    for (const relation of tracking.relations ?? []) {
      if (focus.has(relation.fromTrackedItemId) && !focus.has(relation.toTrackedItemId)) {
        focus.add(relation.toTrackedItemId);
        dependencyGrew = true;
      }
      if (focus.has(relation.toTrackedItemId) && !focus.has(relation.fromTrackedItemId)) {
        focus.add(relation.fromTrackedItemId);
        dependencyGrew = true;
      }
    }
  }
  return focus;
}
