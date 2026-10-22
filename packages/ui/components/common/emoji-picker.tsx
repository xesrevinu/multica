"use client";

import { useEffect, useRef, useCallback } from "react";

interface EmojiPickerProps {
  onSelect: (emoji: string) => void;
}

export function EmojiPicker({ onSelect }: EmojiPickerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  const handleSelect = useCallback((emoji: { native: string }) => {
    onSelectRef.current(emoji.native);
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let cancelled = false;
    void Promise.all([import("@emoji-mart/data"), import("emoji-mart")]).then(
      ([dataModule, pickerModule]) => {
        if (cancelled) return;
        const picker = new pickerModule.Picker({
          data: dataModule.default,
          onEmojiSelect: handleSelect,
          theme: "auto",
          set: "native",
          previewPosition: "none",
          skinTonePosition: "search",
          maxFrequentRows: 2,
        });
        container.appendChild(picker as unknown as Node);
      },
    );

    return () => {
      cancelled = true;
      container.replaceChildren();
    };
  }, [handleSelect]);

  return <div ref={containerRef} />;
}
