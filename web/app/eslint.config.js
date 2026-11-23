import tseslint from "typescript-eslint";
import boundaries from "./eslint-rules/layers.js";

// The server render runs in goja: nothing on its path reads a browser or Node global.
// Views reach the DOM only through their events and effects, which never run there.
const SERVER_GLOBAL_NAMES = ["window", "document", "navigator", "location", "history", "process", "Buffer", "require"];
const NOT_ON_THE_SERVER = SERVER_GLOBAL_NAMES.map((name) => ({
  name,
  message: "This code runs in the server render (goja): it has no browser or Node globals.",
}));
// The same globals reached through the global object (globalThis.document, global.process).
const SERVER_SYNTAX = [
  {
    selector: `MemberExpression[object.name=/^(globalThis|global|self)$/][property.name=/^(${SERVER_GLOBAL_NAMES.join("|")})$/]`,
    message: "This code runs in the server render (goja): it has no browser or Node globals.",
  },
];

// ADR 0106: data from outside crosses as a DTO. Outside the DTO classes and the
// low-level transport, no code touches the network, storage or a raw payload.
//
// What this does not catch, and review must: a key computed at run time (x[key]),
// a global reached through a spread, Object.assign or a function argument, and
// anything behind `any`. Aliasing a banned name by destructuring, an alias of JSON,
// template literal keys, Reflect, an uncalled dispatchEvent or postMessage, eval,
// new Function, and the other transports (XMLHttpRequest, WebSocket, EventSource,
// sendBeacon) are caught here.
const RAW_DATA_GLOBALS = ["fetch", "localStorage", "sessionStorage", "indexedDB", "XMLHttpRequest", "WebSocket", "EventSource"].map((name) => ({
  name,
  message: "Outside data crosses as a DTO (ADR 0106): only a DTO and the transport touch it.",
}));
const NETWORK_AND_STORAGE = "fetch|localStorage|sessionStorage|indexedDB|cookie|XMLHttpRequest|WebSocket|EventSource|sendBeacon";
const NETWORK_MESSAGE = "Outside data crosses as a DTO (ADR 0106): only the transport touches the network, storage and the channels.";
// Banned in a DTO too: a DTO only reads and writes its own payload.
const NETWORK_SYNTAX = [
  // postMessage and dispatchEvent, called or not (passed to apply, stored).
  { selector: "MemberExpression[property.name=/^(postMessage|dispatchEvent)$/]", message: NETWORK_MESSAGE },
  { selector: "Identifier[name=/^(postMessage|dispatchEvent)$/]:not(Property > Identifier.key):not(MemberExpression > Identifier.property)", message: NETWORK_MESSAGE },
  // The same globals through an object that holds them (globalThis.fetch, window.localStorage, self.fetch, document.cookie).
  { selector: `MemberExpression[property.name=/^(${NETWORK_AND_STORAGE})$/]`, message: NETWORK_MESSAGE },
  { selector: `MemberExpression[computed=true][property.value=/^(${NETWORK_AND_STORAGE}|postMessage|dispatchEvent)$/]`, message: NETWORK_MESSAGE },
  { selector: `MemberExpression[computed=true][property.type='TemplateLiteral'][property.expressions.length=0][property.quasis.0.value.raw=/^(${NETWORK_AND_STORAGE}|postMessage|dispatchEvent)$/]`, message: NETWORK_MESSAGE },
  // Pulled out of an object: const { fetch } = globalThis, const { localStorage: s } = window.
  { selector: `ObjectPattern > Property[key.name=/^(${NETWORK_AND_STORAGE}|postMessage|dispatchEvent)$/]`, message: NETWORK_MESSAGE },
  { selector: `ObjectPattern > Property[key.value=/^(${NETWORK_AND_STORAGE}|postMessage|dispatchEvent)$/]`, message: NETWORK_MESSAGE },
  // Reflect reaches any member by name, and nothing here needs it.
  { selector: "Identifier[name='Reflect']", message: "Reflect reaches banned members by name (ADR 0106)." },
  // A Response's body is read in the transport.
  { selector: "CallExpression[callee.property.name='json']", message: "Read a response body in the transport and hand it to a DTO (ADR 0106)." },
];
const RAW_DATA = "is raw data: read it in the transport and hand it to a DTO (ADR 0106).";
// A DTO may read these: they are its job.
const PAYLOAD_SYNTAX = [
  // `.data` and `.detail` of anything, through a cast too: (m as MessageEvent).data.
  { selector: "MemberExpression[property.name=/^(data|detail)$/]", message: `\`.data\` and \`.detail\` ${RAW_DATA}` },
  { selector: "MemberExpression[computed=true][property.value=/^(data|detail)$/]", message: `\`.data\` and \`.detail\` ${RAW_DATA}` },
  { selector: "MemberExpression[computed=true][property.type='TemplateLiteral'][property.expressions.length=0][property.quasis.0.value.raw=/^(data|detail)$/]", message: `\`.data\` and \`.detail\` ${RAW_DATA}` },
  // Destructured, in parameters and in declarations.
  { selector: "ObjectPattern > Property[key.name=/^(data|detail)$/]", message: `A destructured \`data\` or \`detail\` ${RAW_DATA}` },
  { selector: "ObjectPattern > Property[key.value=/^(data|detail)$/]", message: `A destructured \`data\` or \`detail\` ${RAW_DATA}` },
  // JSON through an alias or the global object: const J = JSON, globalThis.JSON.
  { selector: "Identifier[name='JSON']:not(MemberExpression[object.name='JSON'] > Identifier.object)", message: "Use JSON only as JSON.parse and JSON.stringify, and only in the transport (ADR 0106)." },
  { selector: "MemberExpression[property.name='JSON']", message: "Use JSON only as JSON.parse and JSON.stringify, and only in the transport (ADR 0106)." },
];
const JSON_PROPERTIES = [
  { object: "JSON", property: "parse", message: "Parse in the transport and hand the result to a DTO (ADR 0106)." },
  { object: "JSON", property: "stringify", message: "Send a DTO: the transport stringifies it (ADR 0106)." },
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
  "src/features/product-owner/infrastructure/browser-dock.ts",
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
      "no-eval": "error",
      "no-new-func": "error",
      "no-restricted-globals": ["error", ...RAW_DATA_GLOBALS],
      "no-restricted-properties": ["error", ...JSON_PROPERTIES],
      "no-restricted-syntax": ["error", ...NETWORK_SYNTAX, ...PAYLOAD_SYNTAX],
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
      // DTOs run in the server render too (the props DTO reads the island props there).
      ...DTO_FILES,
    ],
    ignores: TEST_FILES,
    rules: {
      "no-restricted-globals": ["error", ...RAW_DATA_GLOBALS, ...NOT_ON_THE_SERVER],
      "no-restricted-syntax": ["error", ...NETWORK_SYNTAX, ...PAYLOAD_SYNTAX, ...SERVER_SYNTAX],
    },
  },
  {
    // A DTO's own fromJSON and toJSON read and write the raw payload, but never reach the network, storage, a channel or a global.
    files: DTO_FILES,
    ignores: TEST_FILES,
    rules: { "no-restricted-properties": "off", "no-restricted-syntax": ["error", ...NETWORK_SYNTAX, ...SERVER_SYNTAX] },
  },
  {
    files: [...TRANSPORT_FILES, ...TEST_FILES],
    rules: { "no-restricted-globals": "off", "no-restricted-properties": "off", "no-restricted-syntax": "off" },
  },
);
