import { docsHrefForLocale } from "@/lib/docs-href";
import { createEnDict } from "./en";
import { createJaDict } from "./ja";
import { createKoDict } from "./ko";
import { createZhDict } from "./zh";
import {
  toLandingDictionaryLocale,
  type LandingDict,
  type Locale,
} from "./types";

/** Server-only: static imports stay out of the landing client graph. */
export function createLandingDict(locale: Locale, allowSignup: boolean): LandingDict {
  // Locales without their own landing copy reuse another dictionary, but docs
  // links still follow the viewer's locale.
  const docsHref = docsHrefForLocale(locale);
  switch (toLandingDictionaryLocale(locale)) {
    case "ja":
      return createJaDict(allowSignup, docsHref);
    case "ko":
      return createKoDict(allowSignup, docsHref);
    case "zh":
      return createZhDict(allowSignup, docsHref);
    default:
      return createEnDict(allowSignup, docsHref);
  }
}
