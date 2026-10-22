import type { NextConfig } from "next";
import { config } from "dotenv";
import { resolve } from "path";
import {
  resolveDevRemoteApiUrl,
  resolveRemoteApiUrl,
} from "./config/runtime-urls";
// Load root .env so local next.config.ts rewrites see REMOTE_API_URL.
// Production requests use proxy.ts runtime rewrites, which read process.env
// when the Next.js server runs instead of baking these URLs at build time.
config({ path: resolve(__dirname, "../../.env") });

// `next dev` falls back to the conventional localhost upstreams; builds use
// the strict resolvers so prebuilt images keep unset upstreams unproxied.
const isDev = process.env.NODE_ENV === "development";
const remoteApiUrl = isDev
  ? resolveDevRemoteApiUrl(process.env)
  : resolveRemoteApiUrl(process.env);

// Parse hostnames from CORS_ALLOWED_ORIGINS so that Next.js dev server
// allows cross-origin HMR / webpack requests (e.g. from Tailscale IPs).
const allowedDevOrigins = process.env.CORS_ALLOWED_ORIGINS
  ? process.env.CORS_ALLOWED_ORIGINS.split(",")
      .map((origin) => {
        try {
          return new URL(origin.trim()).host;
        } catch {
          return origin.trim();
        }
      })
      .filter(Boolean)
  : undefined;

const nextConfig: NextConfig = {
  ...(process.env.STANDALONE === "true" ? { output: "standalone" as const } : {}),
  transpilePackages: ["@multica/core", "@multica/ui", "@multica/views", "agentation"],
  // Browser-only widgets. The client bundle still loads them on demand;
  // the server must not inline mermaid, charts, or the emoji picker.
  serverExternalPackages: [
    "mermaid",
    "cytoscape",
    "recharts",
    "emoji-mart",
    "@emoji-mart/data",
  ],
  experimental: {
    // lucide-react / date-fns are already on Next's default list. Base UI is
    // not, and the landing page used to pull it in via views barrels.
    optimizePackageImports: ["@base-ui/react"],
  },
  ...(allowedDevOrigins && allowedDevOrigins.length > 0
    ? { allowedDevOrigins }
    : {}),
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [75, 80, 85],
  },
  async rewrites() {
    return {
      afterFiles: [
        ...(isDev
          ? [
              {
                source: "/__agentation/:path*",
                destination: "http://127.0.0.1:4747/:path*",
              },
            ]
          : []),
        ...(remoteApiUrl
          ? [
              {
                source: "/v1/:path*",
                destination: `${remoteApiUrl}/v1/:path*`,
              },
              {
                source: "/api/:path*",
                destination: `${remoteApiUrl}/api/:path*`,
              },
              {
                source: "/ws",
                destination: `${remoteApiUrl}/ws`,
              },
              {
                source: "/health",
                destination: `${remoteApiUrl}/health`,
              },
              {
                source: "/auth/:path*",
                destination: `${remoteApiUrl}/auth/:path*`,
              },
              {
                source: "/uploads/:path*",
                destination: `${remoteApiUrl}/uploads/:path*`,
              },
            ]
          : []),
      ],
      fallback: [],
    };
  },
};

export default nextConfig;
