import tseslint from "typescript-eslint";
import boundaries from "./eslint-rules/layers.js";

// The server render runs in goja: nothing on its path reads a browser or Node global.
// Views reach the DOM only through their events and effects, which never run there.
const NOT_ON_THE_SERVER = ["window", "document", "navigator", "location", "history", "process", "Buffer", "require"].map((name) => ({
  name,
  message: "This code runs in the server render (goja): it has no browser or Node globals.",
}));

// ADR 0106: data from outside crosses as a DTO. Outside the DTO classes and the
// low-level transport, no code touches a raw payload: it calls none of these, and
// reads no `event.data` or `event.detail`.
const RAW_DATA_GLOBALS = ["fetch", "localStorage", "sessionStorage"].map((name) => ({
  name,
  message: "Outside data crosses as a DTO (ADR 0106): only a DTO and the transport touch it.",
}));
const RAW_DATA_SYNTAX = [
  { selector: "CallExpression[callee.property.name='postMessage']", message: "postMessage carries a DTO's toJSON(), and only the transport calls it (ADR 0106)." },
  { selector: "CallExpression[callee.property.name='dispatchEvent']", message: "dispatchEvent with a payload is the transport's (ADR 0106)." },
  { selector: "CallExpression[callee.name='dispatchEvent']", message: "dispatchEvent with a payload is the transport's (ADR 0106)." },
  { selector: "MemberExpression[object.name=/^(event|evt|ev|e)$/][property.name=/^(data|detail)$/]", message: "event.data and event.detail are raw data: read them in the transport and hand them to a DTO (ADR 0106)." },
  { selector: "ArrowFunctionExpression > ObjectPattern.params > Property[key.name=/^(data|detail)$/]", message: "event.data and event.detail are raw data: read them in the transport and hand them to a DTO (ADR 0106)." },
];

// The DTO classes, and the low-level transport that feeds them: each touches only the raw
// payload and hands it straight to a DTO (ADR 0106). Named here once.
const DTO_FILES = ["src/**/dto/**"];
const TRANSPORT_FILES = [
  "src/shared/infrastructure/api-client.ts",
  "src/shared/infrastructure/stream-feed.ts",
  "src/shared/infrastructure/shared-feed.ts",
  "src/shared/infrastructure/island-props.ts",
  "src/features/navigation/infrastructure/browser-navigation-signal.ts",
];
// Tests and their helpers build raw data on purpose.
const TEST_FILES = ["**/*.test.ts", "**/testing/**"];

export default tseslint.config(
  { ignores: ["node_modules/**", "dist/**"] },
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries },
    rules: {
      "boundaries/layers": "error",
      "no-restricted-globals": ["error", ...RAW_DATA_GLOBALS],
      "no-restricted-properties": [
        "error",
        { object: "JSON", property: "parse", message: "Parse in the transport and hand the result to a DTO (ADR 0106)." },
        { object: "JSON", property: "stringify", message: "Send a DTO: the transport stringifies it (ADR 0106)." },
      ],
      "no-restricted-syntax": ["error", ...RAW_DATA_SYNTAX],
    },
  },
  {
    files: [
      "src/islands/**",
      "src/composition/server-root.tsx",
      "src/composition/top-bar-view.tsx",
      "src/composition/top-bar-presenters.ts",
      "src/features/*/domain/**",
      "src/features/*/application/**",
      "src/features/*/presentation/**",
      "src/shared/kernel/**",
      "src/shared/presentation/**",
    ],
    ignores: TEST_FILES,
    rules: { "no-restricted-globals": ["error", ...RAW_DATA_GLOBALS, ...NOT_ON_THE_SERVER] },
  },
  {
    files: [...DTO_FILES, ...TRANSPORT_FILES, ...TEST_FILES],
    rules: { "no-restricted-globals": "off", "no-restricted-properties": "off", "no-restricted-syntax": "off" },
  },
);
