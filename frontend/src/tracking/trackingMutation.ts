/**
 * Mutation orchestration shared by every Tracking command. The mutation runs
 * exactly once; only the authoritative reload may be retried. A committed
 * mutation whose refresh fails is reported distinctly and is never re-sent.
 */
export type TrackingMutationOutcome =
  | { status: 'ok' }
  | { status: 'mutationFailed'; message: string }
  | { status: 'refreshFailed'; message: string };

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export async function runTrackingMutation(
  mutate: () => Promise<unknown>,
  reload: () => Promise<void>,
): Promise<TrackingMutationOutcome> {
  try {
    await mutate();
  } catch (error) {
    return { status: 'mutationFailed', message: messageOf(error) };
  }
  try {
    await reload();
  } catch (error) {
    return { status: 'refreshFailed', message: messageOf(error) };
  }
  return { status: 'ok' };
}
