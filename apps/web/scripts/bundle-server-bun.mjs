/**
 * Post-process the vinext server into one Bun executable with ESM bytecode.
 * Vinext's build is unchanged. Static client assets stay on disk next to the
 * binary (dist/client). `--bytecode` without `compile` is CJS and rejects
 * `import.meta`; `compile` emits an executable, and the plugin rewrites
 * `import.meta` to the source file's real path before that parse.
 */
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const root = path.resolve(import.meta.dirname, "..");
const serverDir = path.join(root, "dist/server");
const outfile = path.join(serverDir, "multica-web");

function rewriteImportMeta(contents, filename) {
  if (!contents.includes("import.meta")) return contents;
  const dir = path.dirname(filename);
  const url = pathToFileURL(filename).href;
  let next = contents;
  next = next.replaceAll("import.meta.filename", JSON.stringify(filename));
  next = next.replaceAll("import.meta.dirname", JSON.stringify(dir));
  next = next.replaceAll("import.meta.dir", JSON.stringify(dir));
  next = next.replaceAll("import.meta.url", JSON.stringify(url));
  next = next.replaceAll("import.meta.env", "process.env");
  if (next.includes("import.meta")) {
    const meta = `({url:${JSON.stringify(url)},filename:${JSON.stringify(filename)},dir:${JSON.stringify(dir)},dirname:${JSON.stringify(dir)},env:process.env})`;
    next = next.replaceAll("import.meta", meta);
  }
  return next;
}

// One physical React for the whole executable. pnpm keeps older copies for
// other packages; bundling those next to react-dom splits the dispatcher
// and SSR hooks crash. Node's external resolution hid that by walking to
// the app's single copy.
const appRequire = createRequire(path.join(root, "package.json"));
const pinnedSpecifiers = /^(react-server-dom-webpack|react-dom|react|scheduler)(\/|$)/;

function pinReact(specifier) {
  if (!pinnedSpecifiers.test(specifier)) return null;
  try {
    return fs.realpathSync(appRequire.resolve(specifier));
  } catch {
    return null;
  }
}

const bundlePlugin = {
  name: "vinext-bun-executable",
  setup(build) {
    build.onResolve({ filter: pinnedSpecifiers }, (args) => {
      const pinned = pinReact(args.path);
      if (!pinned) return undefined;
      return { path: pinned };
    });
    build.onLoad({ filter: /\.[cm]?js$/ }, async (args) => {
      const file = Bun.file(args.path);
      if (!(await file.exists())) return undefined;
      let contents = await file.text();
      const isProdServer = args.path.endsWith(`${path.sep}server${path.sep}prod-server.js`);
      if (contents.includes("createRequire") && contents.includes("e(import.meta.url)")) {
        // Rolldown's runtime require() loads react from disk. Serve the
        // bundled copy instead so SSR and that require share one dispatcher.
        contents = contents.replaceAll(
          "e(import.meta.url)",
          "((id) => globalThis.__MULTICA_CJS__?.[id] ?? e(import.meta.url)(id))",
        );
      }
      if (isProdServer) {
        contents = contents.replace(
          "() => import(resolveServerEntryImportUrl(entryPath))",
          "() => Promise.resolve(globalThis.__VINEXT_BUNDLED_SERVER_ENTRY)",
        );
        contents = contents.replace(
          "const isAppRouter = fs.existsSync(rscEntryPath);",
          "const isAppRouter = globalThis.__VINEXT_BUNDLED_SERVER_ENTRY != null || fs.existsSync(rscEntryPath);",
        );
        // No precompressed files. Skip the in-memory static cache so the
        // request falls through to the path that compresses on the way out.
        contents = contents.replaceAll(
          "const staticCache = await StaticFileCache.create(clientDir);",
          "const staticCache = undefined;",
        );
      }
      if (!contents.includes("import.meta") && !isProdServer) return undefined;
      if (contents.includes("import.meta")) contents = rewriteImportMeta(contents, args.path);
      return { contents, loader: "js" };
    });
  },
};

const entryPath = path.join(serverDir, ".bun-entry.mjs");
const seedPath = path.join(serverDir, ".bun-react-seed.mjs");
// Vinext's Rolldown runtime still calls require("react") at request time.
// Point that require at the bundled module so SSR hooks and the renderer
// share one dispatcher, with no React install beside the executable.
fs.writeFileSync(
  seedPath,
  `import * as React from "react";
import * as ReactDOM from "react-dom";
import * as JSXRuntime from "react/jsx-runtime";

function cjsExports(namespace, marker) {
  if (namespace[marker] != null) return namespace;
  if (namespace.default != null && namespace.default[marker] != null) return namespace.default;
  return namespace.default ?? namespace;
}

globalThis.__MULTICA_CJS__ = {
  react: cjsExports(React, "useState"),
  "react-dom": cjsExports(ReactDOM, "__DOM_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE"),
  "react/jsx-runtime": cjsExports(JSXRuntime, "jsx"),
};
`,
);
fs.writeFileSync(
  entryPath,
  `import ${JSON.stringify(seedPath)};
import path from "node:path";
import { startProdServer } from "vinext/server/prod-server";
import * as serverEntry from ${JSON.stringify(path.join(serverDir, "index.js"))};

globalThis.__VINEXT_BUNDLED_SERVER_ENTRY = serverEntry;

const exeDir = path.dirname(process.execPath);
const PORT = Number.parseInt(process.env.PORT ?? "3000", 10);
const HOST = process.env.HOST ?? "0.0.0.0";

const { server } = await startProdServer({
  port: PORT,
  host: HOST,
  outDir: path.resolve(exeDir, ".."),
});

const isHealth = (req) => {
  const urlPath = (req.url ?? "/").split("?")[0];
  return (req.method === "GET" || req.method === "HEAD") && urlPath === "/health";
};

const existing = server.listeners("request").slice();
server.removeAllListeners("request");
server.on("request", (req, res) => {
  if (isHealth(req)) {
    res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    res.end("ok");
    return;
  }
  for (const listener of existing) listener.call(server, req, res);
});
`,
);

const compileTarget = process.env.MULTICA_WEB_COMPILE_TARGET;
const result = await Bun.build({
  entrypoints: [entryPath],
  compile: compileTarget ? { outfile, target: compileTarget } : { outfile },
  target: "bun",
  format: "esm",
  minify: true,
  bytecode: process.env.MULTICA_WEB_BYTECODE === "0" ? false : true,
  sourcemap: "none",
  packages: "bundle",
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
  },
  plugins: [bundlePlugin],
});

fs.rmSync(entryPath, { force: true });
fs.rmSync(seedPath, { force: true });

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}

const binary = result.outputs.find((item) => item.path === outfile) ?? result.outputs[0];
if (!binary || !fs.existsSync(outfile)) {
  console.error("bun compile produced no executable", result.outputs.map((item) => item.path));
  process.exit(1);
}
fs.chmodSync(outfile, 0o755);
console.log(`bun executable ${outfile} ${(fs.statSync(outfile).size / 1024 / 1024).toFixed(1)} MiB`);
