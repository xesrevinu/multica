"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";

// GitHub avatar URLs sometimes 404 (deleted accounts, ghost users) or are
// blocked when a referrer is sent. A bare <img class="rounded-full"> then
// paints as an empty circle. Only http(s) URLs are tried; everything else
// falls back to an initial on a filled disc.
export function usableAvatarUrl(url?: string | null): string | undefined {
  if (!url) return undefined;
  const trimmed = url.trim();
  if (!trimmed) return undefined;
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return undefined;
    return parsed.href;
  } catch {
    return undefined;
  }
}

export function personInitial(name?: string | null): string {
  const trimmed = name?.trim() ?? "";
  const first = trimmed.charAt(0);
  return first ? first.toUpperCase() : "?";
}

export function PersonAvatar({
  name,
  url,
  className,
}: {
  name?: string | null;
  url?: string | null;
  className?: string;
}) {
  const src = usableAvatarUrl(url);
  const [failed, setFailed] = useState<string | undefined>(undefined);
  const showImage = !!src && failed !== src;
  const disc = "inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-muted ring-1 ring-border";
  if (!showImage) {
    return (
      <span aria-hidden className={cn(disc, "text-[10px] font-medium text-muted-foreground", className)}>
        {personInitial(name)}
      </span>
    );
  }
  return (
    <img
      src={src}
      alt=""
      referrerPolicy="no-referrer"
      loading="lazy"
      decoding="async"
      className={cn(disc, "object-cover", className)}
      onError={() => setFailed(src)}
    />
  );
}
