// compose copies property descriptors, so getters stay getters (a spread
// would freeze them into values); later parts override earlier ones.

/** @param {...object} parts */
export function compose(...parts) {
  const out = {};
  for (const part of parts) Object.defineProperties(out, Object.getOwnPropertyDescriptors(part));
  return out;
}
