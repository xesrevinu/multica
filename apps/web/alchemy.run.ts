import * as Alchemy from "alchemy";
import * as Fly from "alchemy/Fly";
import { Server } from "alchemy/Website/Server";
import * as Effect from "effect/Effect";

const port = Number(process.env.FRONTEND_PORT ?? 3000);

/**
 * Same stack shape as Alchemy's vinext Node examples (`Fly.providers()` +
 * local state). The resource is only `Website.Server` with the vinext Node
 * adapter: `alchemy dev` is vinext's Vite server, `alchemy deploy` writes
 * `dist/` and `dist/server/serve-node.mjs`. No cloud resource is declared,
 * so this does not talk to Fly.
 */
export const Web = Server("Web", {
  framework: "@alchemy.run/frontend-frameworks/vinext/node",
  target: "@alchemy.run/frontend-frameworks/vinext/node",
  // Alchemy's CLI forces NODE_ENV=production for its own renderer. Vinext
  // inherits that and SSR then loads production react-dom while Vite still
  // compiles jsxDEV, which crashes with "dispatcher.getOwner is not a function".
  env: {
    NODE_ENV: "development",
  },
  dev: {
    port: Number.isInteger(port) && port > 0 ? port : 3000,
    host: process.env.HOST ?? "0.0.0.0",
    strictPort: true,
  },
});

export default Alchemy.Stack(
  "MulticaWeb",
  {
    providers: Fly.providers(),
    state: Alchemy.localState(),
  },
  Effect.gen(function* () {
    const site = yield* Web;
    return {
      url: site.url,
      distDir: site.distDir,
      serverEntry: site.serverEntry,
    };
  }),
);
