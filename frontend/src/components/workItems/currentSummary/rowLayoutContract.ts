import { alpha, type Theme } from '@mui/material/styles';

/**
 * Current Work Item row — LEFT/CENTER/RIGHT width allocation contract.
 *
 * A single source of truth so the "center never dominates the row" rule is
 * a checkable number rather than a CSS string nobody notices drifting.
 * `WorkItemRow.tsx` builds its grid template directly from these
 * fractions; frontend tests assert the numeric contract here instead of
 * parsing rendered CSS.
 *
 * LEFT < CENTER ≈ RIGHT. LEFT is identity (title, one lifecycle tag, final
 * target); CENTER is only a thumbnail of the workflow pattern and matches the
 * RIGHT reading area (summary, exact tracked-work counts, Attention text) in
 * width without becoming a detail graph. Deliberately not 33/33/33.
 */
export const ROW_COLUMN_FRACTIONS = {
  left: 0.23,
  center: 0.385,
  right: 0.385,
} as const;

/** CENTER and RIGHT stay within this absolute difference of each other. */
export const ROW_CENTER_RIGHT_TOLERANCE = 0.03;

export function buildRowGridTemplateColumns(): string {
  const { left, center, right } = ROW_COLUMN_FRACTIONS;
  return `${left}fr ${center}fr ${right}fr`;
}

/**
 * A soft, low-contrast column separator — built from the existing `divider`
 * design token (never a new hardcoded color), just reduced in opacity so
 * the three columns read as a gentle rhythm rather than a spreadsheet/table
 * grid. Used for both the LEFT|CENTER and CENTER|RIGHT separators.
 */
export function buildColumnDividerBorderColor(theme: Theme): string {
  return alpha(theme.palette.divider, 0.55);
}
