import { Effect, Option, Tracer } from "effect"

const traceparentRe = /^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$/i

function formatTraceparent(span: { readonly traceId: string; readonly spanId: string; readonly sampled?: boolean }) {
  const sampled = span.sampled === false ? "00" : "01"
  return `00-${span.traceId}-${span.spanId}-${sampled}`
}

export const currentTraceparent: Effect.Effect<string | undefined> = Effect.map(
  Effect.serviceOption(Tracer.ParentSpan),
  (parent): string | undefined =>
    Option.isSome(parent) ? formatTraceparent(parent.value) : process.env.TRACEPARENT,
)
