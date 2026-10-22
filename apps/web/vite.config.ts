import { config as loadDotenv } from "dotenv";
import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import vinext from "vinext";

// Same root env file next.config.ts reads, so dev rewrites and Vite see one source.
loadDotenv({ path: resolve(import.meta.dirname, "../../.env") });

// Tailscale / LAN HMR. Vite blocks unknown Host headers unless they are listed.
const allowedHosts = process.env.CORS_ALLOWED_ORIGINS
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

export default defineConfig({
  plugins: [
    tailwindcss(),
    // Prerender every route that does not read the request. Client JS/CSS
    // still land in dist/client as hashed static files either way.
    // Compression is the HTTP server's job, not a second copy of every asset.
    vinext({ prerender: true, precompress: false }),
  ],
  server: {
    ...(allowedHosts && allowedHosts.length > 0 ? { allowedHosts } : {}),
  },
});
