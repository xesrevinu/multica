/**
 * Post-process the vinext server into one Bun executable with ESM bytecode.
 * Vinext's build is unchanged. Static client assets stay on disk next to the
 * binary (dist/client). `--bytecode` without `compile` is CJS and rejects
 * `import.meta`; `compile` emits an executable, and the plugin rewrites
 * `import.meta` to the source file's real path before that parse.
 */
import fs from "node:fs";
import path from "node:path";
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

const bundlePlugin = {
  name: "vinext-bun-executable",
  setup(build) {
    build.onLoad({ filter: /\.[cm]?js$/ }, async (args) => {
      const file = Bun.file(args.path);
      if (!(await file.exists())) return undefined;
      let contents = await file.text();
      const isProdServer = args.path.endsWith(`${path.sep}server${path.sep}prod-server.js`);
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
fs.writeFileSync(
  entryPath,
  `import path from "node:path";
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

const result = await Bun.build({
  entrypoints: [entryPath],
  compile: { outfile },
  target: "bun",
  format: "esm",
  minify: true,
  bytecode: true,
  sourcemap: "none",
  packages: "bundle",
  // React's server renderer and the component tree must share one module
  // instance. Bundling them inlines two copies and SSR hooks crash.
  external: [
    "react",
    "react-dom",
    "react/jsx-runtime",
    "react/jsx-dev-runtime",
    "react/compiler-runtime",
    "scheduler",
  ],
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
  },
  plugins: [bundlePlugin],
});

fs.rmSync(entryPath, { force: true });

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
