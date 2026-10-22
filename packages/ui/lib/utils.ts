import { createCn, type ClassValue } from "cnfast"

/**
 * The type scale's steps are role-named (`text-body`, `text-caption`), so a
 * default Tailwind merge cannot tell them apart from a colour utility:
 * `text-<x>` is ambiguous, and the built-in table only lists Tailwind's
 * default sizes. Left unconfigured it files every role step under
 * text-colour, and then drops whichever of `text-body` /
 * `text-muted-foreground` came first as a conflict.
 *
 * Registering the steps as font-size restores the real conflict groups: size
 * beats size, colour beats colour, and the two coexist. Keep this list in
 * sync with the `--text-*` steps in `packages/ui/styles/tokens.css`.
 */
export const cn = createCn({
  extend: {
    classGroups: {
      "font-size": [
        {
          text: [
            "micro",
            "caption",
            "label",
            "body",
            "body-lg",
            "title-sm",
            "title",
            "title-lg",
            "display-sm",
            "display",
          ],
        },
      ],
    },
  },
})

export type { ClassValue }
