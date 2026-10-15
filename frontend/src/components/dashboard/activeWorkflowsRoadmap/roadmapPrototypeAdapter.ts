/**
 * Dashboard Active Workflows Roadmap — frontend PROTOTYPE data adapter.
 *
 * BOUNDARY (read before touching this file):
 * This is the ONLY file in the Roadmap module allowed to import the
 * frontend WorkItem prototype fixtures. It exists so that "this row's date
 * came from a design-prototype fixture" and "this row's date is real
 * backend-authoritative Dashboard truth" can never be confused by a reader:
 * everything this file returns is prototype-only, and nothing here is wired
 * into `useDashboardView`/`dashboardApi`/the production Dashboard DTO.
 *
 * DATE SOURCE HONESTY:
 * - The three real ACTIVE prototype WorkItems below (`wi-checkout-platform`,
 *   `wi-dist-systems`, `wi-lob-onboarding`) reuse the SAME `targetDate`
 *   already shown for them elsewhere (Work Items page / Dependency
 *   Explorer) as their `currentTargetDate`. Nothing is fabricated for a
 *   real prototype WorkItem's target.
 * - `startDate` and `previousTargetDate` below are ADDITIONAL
 *   roadmap-presentation-only metadata (Experiment 1.1): these WorkItems
 *   have no real start/prior-target field anywhere in the fixture data or
 *   backend, so explicit literal presentation dates are chosen here,
 *   consistent with each WorkItem's own narrative (e.g. Checkout's history
 *   already narrates a Sep 10 architecture kickoff and a target that moved
 *   out under schedule pressure). They are never read off
 *   `createdAt`/`updatedAt`, and they are never written back to the shared
 *   `PROTOTYPE_WORKFLOW_MODELS`/`PROTOTYPE_WORK_ITEMS` fixtures.
 * - `description` is reused verbatim from the same WorkItem's existing
 *   prototype description — never a second, independently-authored blurb.
 *
 * PRODUCTION STATUS:
 * `roadmapAuthoritativeAdapter.ts` now supplies production membership and
 * identity from Dashboard truth. This prototype adapter remains isolated to
 * visual/date tests. A future authoritative Workflow/Monitor schedule can
 * enrich the production sibling adapter without changing the renderer.
 */

import { PROTOTYPE_WORKFLOW_MODELS } from '../../workItems/fixtures/workItemPrototypeFixtures.ts';
import { resolveRoadmapRowTimelineMode, type ActiveWorkflowRoadmapRow } from './roadmapLayout.ts';

/**
 * Roadmap-presentation-only schedule metadata for a real, active,
 * navigable prototype WorkItem. Lives here (not in the shared WorkItem
 * fixture) because it is Dashboard Roadmap presentation data, not an
 * authoritative WorkItem/WorkflowModel field.
 */
interface RoadmapPrototypeSchedule {
  workItemId: string;
  startDate: string;
  /** Reuses the WorkItem's own real `targetDate` — never a second, invented number. */
  currentTargetDate: string;
  /** Optional: V1 supports only "current + one prior" target, not a full revision history. */
  previousTargetDate?: string;
}

const REAL_WORKFLOW_SCHEDULES: RoadmapPrototypeSchedule[] = [
  {
    workItemId: 'wi-checkout-platform',
    startDate: '2026-09-03',
    currentTargetDate: PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!.targetDate!,
  },
  {
    workItemId: 'wi-dist-systems',
    startDate: '2026-09-10',
    currentTargetDate: PROTOTYPE_WORKFLOW_MODELS['wi-dist-systems']!.targetDate!,
  },
  {
    workItemId: 'wi-lob-onboarding',
    startDate: '2026-09-16',
    // The Adoption-gate target slipped from an earlier Oct 2 estimate while
    // staging validation ran longer than planned — a realistic, restrained
    // revision example on a REAL, navigable WorkItem rather than a
    // synthetic row.
    previousTargetDate: '2026-10-02',
    currentTargetDate: PROTOTYPE_WORKFLOW_MODELS['wi-lob-onboarding']!.targetDate!,
  },
];

function realPrototypeRoadmapRows(): ActiveWorkflowRoadmapRow[] {
  return REAL_WORKFLOW_SCHEDULES.map((schedule) => {
    const model = PROTOTYPE_WORKFLOW_MODELS[schedule.workItemId]!;
    return {
      workItemId: schedule.workItemId,
      title: model.title,
      description: model.description ?? null,
      timeline: resolveRoadmapRowTimelineMode(
        schedule.startDate,
        schedule.currentTargetDate,
        schedule.previousTargetDate,
      ),
    };
  });
}

/**
 * A single, clearly-labeled synthetic supplementary row covering the
 * unscheduled presentation mode, which the real fixture set cannot
 * honestly demonstrate today (every real active WorkItem already has a
 * target). Not a real WorkItem: its id is never present in
 * `PROTOTYPE_WORK_ITEMS`, so it cannot and does not resolve to a real
 * WorkItem detail surface this round. Kept to exactly one row so the real,
 * navigable workflows remain the visual-acceptance body of the panel.
 */
function syntheticSupplementaryRoadmapRows(): ActiveWorkflowRoadmapRow[] {
  return [
    {
      workItemId: 'wi-roadmap-demo-unscheduled-planning',
      title: 'Q4 Platform Planning Spike (prototype only)',
      description: 'Not yet scheduled — placeholder for early-stage planning discussion.',
      timeline: resolveRoadmapRowTimelineMode(null, null),
    },
  ];
}

/** Builds the full prototype Active Workflows Roadmap row set (V1: static). */
export function buildPrototypeRoadmapRows(): ActiveWorkflowRoadmapRow[] {
  return [...realPrototypeRoadmapRows(), ...syntheticSupplementaryRoadmapRows()];
}

const REAL_PROTOTYPE_WORK_ITEM_IDS = REAL_WORKFLOW_SCHEDULES.map((schedule) => schedule.workItemId);

/** Real prototype WorkItem ids are the only ones safe to hand to `onOpenWorkItem`. */
export function isRealPrototypeWorkItemId(workItemId: string): boolean {
  return REAL_PROTOTYPE_WORK_ITEM_IDS.includes(workItemId);
}
