/**
 * Builds a creation-route URL with params that must survive a hop between screens.
 *
 * `runtime` is a seed, not an identity: a conversation joins the sessions list
 * only once it holds a message or a saved configuration, so for the first turn
 * the runtime the user just picked cannot be read back from anywhere. It rides
 * in the URL rather than in component state so a refresh on that first turn
 * still knows where the conversation runs.
 *
 * Squads were removed from the product (orchestrator teams replace them). The
 * `squad` query key is ignored if present so old bookmarks do not break.
 */
export function createPathWithParams(
  path: string,
  params: { squad?: string | null; runtime?: string | null },
): string {
  const query = new URLSearchParams();
  if (params.runtime) query.set("runtime", params.runtime);
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

/** @deprecated Squads removed — returns path unchanged (drops squad query). */
export function withSquadParam(path: string, _squadId: string | null): string {
  return createPathWithParams(path, {});
}
