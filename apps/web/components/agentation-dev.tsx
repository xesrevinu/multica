"use client";

import { Agentation } from "agentation";

/**
 * Dev-only visual feedback overlay. Annotate DOM nodes and hand the
 * structured dump to the coding agent. Omitted from production builds
 * by the NODE_ENV guard at the call site in `app/layout.tsx`.
 */
export function AgentationDev() {
  return <Agentation endpoint="/__agentation" />;
}
