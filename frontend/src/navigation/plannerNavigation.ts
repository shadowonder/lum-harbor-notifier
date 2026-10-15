import type { PlannerRailMode } from '../pages/plannerData';

/**
 * One-shot navigation state used to open Planner from another application
 * surface without introducing a router or persisting a PlannerItem snapshot.
 *
 * selectedPlannerItemId is resolved from Planner's freshly loaded projections.
 * selectedDate remains a local YYYY-MM-DD value and is omitted for no-date and
 * past-due queue navigation, where Open mode owns the temporal context.
 */
export interface PlannerNavigationIntent {
  selectedPlannerItemId?: string;
  selectedDate?: string;
  preferredRailMode: PlannerRailMode;
}
