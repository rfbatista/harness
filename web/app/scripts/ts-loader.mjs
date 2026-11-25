// A Node module loader that transpiles TypeScript with esbuild (no type check:
// `npm run typecheck` does that) and resolves extensionless relative imports.
import { readFile } from "node:fs/promises";
import { existsSync, statSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { transform } from "esbuild";

const EXTENSIONS = [".ts", ".tsx"];

function candidate(path) {
  if (existsSync(path) && statSync(path).isFile()) return path;
  for (const ext of EXTENSIONS) if (existsSync(path + ext)) return path + ext;
  for (const ext of EXTENSIONS) if (existsSync(`${path}/index${ext}`)) return `${path}/index${ext}`;
  return null;
}

export async function resolve(specifier, context, next) {
  if ((specifier.startsWith("./") || specifier.startsWith("../")) && context.parentURL?.startsWith("file:")) {
    const found = candidate(fileURLToPath(new URL(specifier, context.parentURL)));
    if (found) return { url: pathToFileURL(found).href, shortCircuit: true };
  }
  return next(specifier, context);
}

export async function load(url, context, next) {
  if (!url.endsWith(".ts") && !url.endsWith(".tsx")) return next(url, context);
  const source = await readFile(fileURLToPath(url), "utf8");
  const { code } = await transform(source, {
    loader: url.endsWith(".tsx") ? "tsx" : "ts",
    format: "esm",
    jsx: "automatic",
    target: "es2022",
    sourcefile: fileURLToPath(url),
    sourcemap: "inline",
  });
  return { format: "module", source: code, shortCircuit: true };
}
