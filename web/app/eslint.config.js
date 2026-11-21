import tseslint from "typescript-eslint";
import boundaries from "./eslint-rules/layers.js";

export default tseslint.config(
  { ignores: ["node_modules/**", "dist/**"] },
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries },
    rules: { "boundaries/layers": "error" },
  },
);
