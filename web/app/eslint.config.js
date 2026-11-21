import tseslint from "typescript-eslint";
import boundaries from "./eslint-rules/layers.js";

// The server render runs in goja: nothing on its path reads a browser or Node global.
// (Views touch the DOM only in event handlers and effects, which never run there.)
const NOT_ON_THE_SERVER = ["window", "document", "navigator", "location", "localStorage", "sessionStorage", "history", "process", "Buffer", "require"].map((name) => ({
  name,
  message: "This code runs in the server render (goja): it has no browser or Node globals.",
}));

export default tseslint.config(
  { ignores: ["node_modules/**", "dist/**"] },
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries },
    rules: { "boundaries/layers": "error" },
  },
  {
    files: [
      "src/islands/**",
      "src/composition/server-root.tsx",
      "src/composition/top-bar-view.tsx",
      "src/composition/top-bar-presenters.ts",
      "src/features/*/domain/**",
      "src/features/*/application/**",
      "src/features/*/presentation/presenters/**",
      "src/shared/kernel/**",
    ],
    ignores: ["**/*.test.ts"],
    rules: { "no-restricted-globals": ["error", ...NOT_ON_THE_SERVER] },
  },
);
