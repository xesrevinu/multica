import type { Metadata, Viewport } from "next";
import Script from "next/script";
import { Inter, Geist_Mono, Source_Serif_4 } from "next/font/google";
import { ThemeProvider } from "@/components/theme-provider";
import { StaticAssetSW } from "@/components/static-asset-sw";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { cn } from "@multica/ui/lib/utils";
import { WebProviders } from "@/components/web-providers";
import { RESOURCES } from "@multica/views/locales";
import { getRequestLocale } from "@/lib/request-locale";
import { HTML_LANG } from "@/lib/html-lang";
import { SITE_TITLE, TITLE_TEMPLATE } from "@/platform/document-title";
import {
  resolveBrowserApiBaseUrl,
  resolveBrowserWsUrl,
} from "@/config/runtime-urls";
import { FIND_HIGHLIGHT_CSS } from "@multica/ui/styles/find-highlight";
import "./globals.css";

// Inter is the Latin UI face. next/font produces a hashed family (`__Inter_xxx`)
// plus a synthetic size-adjusted fallback face to prevent FOUT layout shift —
// both are exposed under the `--font-inter` CSS variable.
//
// The full `--font-sans` stack (Inter + the per-locale CJK fallback chain) is
// assembled in static CSS in ./globals.css, not here: it must be overridable per
// `<html lang>` (Japanese Kanji are Han ideographs and need a Japanese-first CJK
// stack), and a hashed family name can only be referenced from CSS via a variable.
// Keeping the CJK chain in CSS also keeps it CSP-safe.
//
// Italic is loaded explicitly: `style` defaults to `["normal"]`, and without a real
// italic face the ~20 semantic italic labels (chat empty states, model-picker's
// "Managed by runtime", dashboard/squad placeholders) plus every markdown <em> and
// blockquote rendered as browser-synthesized oblique.
const inter = Inter({
  subsets: ["latin"],
  style: ["normal", "italic"],
  variable: "--font-inter",
});
// Mono font has no explicit CJK fallback: CJK chars in code blocks are inherently
// non-aligned with a mono grid (Chinese is proportional), so listing CJK fonts
// here would falsely signal alignment guarantees. Browser default fallback handles
// the rare mixed case correctly.
const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  preload: false,
  fallback: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
});
// Editorial serif used for onboarding headlines. Italic support for h1 em
// accents (e.g. "...on one shared board."). Landing overrides `--font-serif`
// with Instrument Serif and does not use this face — do not preload it on
// every route (it would compete with LCP on `/`).
const sourceSerif = Source_Serif_4({
  subsets: ["latin"],
  style: ["normal", "italic"],
  variable: "--font-serif",
  preload: false,
  fallback: [
    "ui-serif",
    "Iowan Old Style",
    "Apple Garamond",
    "Baskerville",
    "Times New Roman",
    "serif",
  ],
});

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#05070b" },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL("https://www.multica.ai"),
  title: {
    default: SITE_TITLE,
    template: TITLE_TEMPLATE,
  },
  description:
    "Source-available platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml" }],
    shortcut: ["/favicon.svg"],
    // iOS never reads the manifest's icons for the home screen; it needs its
    // own opaque, full-bleed square and rounds the corners itself.
    apple: [{ url: "/icons/apple-touch-icon.png", sizes: "180x180" }],
  },
  // Home-screen behaviour: launch without browser chrome, and label the icon
  // "Multica" rather than the long SEO <title>. `capable` renders the
  // standardised `mobile-web-app-capable` tag — Next 16 no longer emits the
  // deprecated apple-prefixed spelling, so iOS standalone rides on the
  // manifest's `display` instead (honoured since iOS 16.4).
  appleWebApp: {
    capable: true,
    title: "Multica",
    // `default` keeps the status bar opaque. viewport-fit=cover still reports
    // env(safe-area-inset-*) so the shell can pad the home indicator and notch.
    statusBarStyle: "default",
  },
  openGraph: {
    type: "website",
    siteName: "Multica",
    locale: "en_US",
  },
  twitter: {
    card: "summary_large_image",
    site: "@multica_hq",
    creator: "@multica_hq",
  },
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
  },
};

// Static import would keep the dev overlay in the production client graph.
// The call site is also folded away when NODE_ENV is production.
async function DevAgentation() {
  const { AgentationDev } = await import("@/components/agentation-dev");
  return <AgentationDev />;
}

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const locale = await getRequestLocale();
  const resources = { [locale]: RESOURCES[locale] };
  const apiBaseUrl = resolveBrowserApiBaseUrl(process.env);
  const wsUrl = resolveBrowserWsUrl(process.env);

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className={cn("antialiased font-sans h-full", inter.variable, geistMono.variable, sourceSerif.variable)}
    >
      <body className="h-full overflow-hidden pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)]">
        {/* LightningCSS cannot parse ::highlight(); see FIND_HIGHLIGHT_CSS. */}
        <style dangerouslySetInnerHTML={{ __html: FIND_HIGHLIGHT_CSS }} />
        {/*
          react-grab: dev-only element inspector. Hold ⌘C (Mac) / Ctrl+C and click
          any element to copy its source path + line + component stack for pasting
          to an AI. Opt-in per developer: only loads when VITE_REACT_GRAB is set in
          a local, gitignored apps/web/.env.local — it never activates for anyone
          else. Both guards are read server-side, so the <Script> is omitted from
          the HTML entirely unless you opted in. The VITE_ prefix matches Vite's
          client-exposed env names. See https://www.react-grab.com/
        */}
        {process.env.NODE_ENV === "development" && process.env.VITE_REACT_GRAB && (
          <Script
            src="//unpkg.com/react-grab/dist/index.global.js"
            crossOrigin="anonymous"
            strategy="beforeInteractive"
          />
        )}
        <ThemeProvider>
          <WebProviders
            locale={locale}
            resources={resources}
            apiBaseUrl={apiBaseUrl}
            wsUrl={wsUrl}
          >
            {children}
          </WebProviders>
          <Toaster />
        </ThemeProvider>
        <StaticAssetSW />
        {process.env.NODE_ENV === "development" ? <DevAgentation /> : null}
      </body>
    </html>
  );
}
