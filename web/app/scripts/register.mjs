// Registers the TypeScript loader for `node --test`.
import { register } from "node:module";

register("./ts-loader.mjs", import.meta.url);
