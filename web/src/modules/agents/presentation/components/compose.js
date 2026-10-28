// Builds an Alpine.data object from parts: each part's fields, getters and
// methods are copied with their descriptors, so a getter stays a getter.

/** @param {object} base @param {...object} parts @returns {object} */
export function compose(base, ...parts) {
  for (const part of parts) Object.defineProperties(base, Object.getOwnPropertyDescriptors(part));
  return base;
}
