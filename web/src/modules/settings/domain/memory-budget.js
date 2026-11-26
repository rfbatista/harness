// The orçamento de memória as people type it: gigabytes, a positive number
// that may have a fraction. The server stores bytes.

export const GB = 1024 ** 3;

/**
 * @param {string} text what was typed, in GB
 * @returns {number | null} bytes, or null when it is not a positive number
 */
export function toBytes(text) {
  const gb = Number(text.trim());
  if (text.trim() === "" || !Number.isFinite(gb) || gb <= 0) return null;
  return Math.round(gb * GB);
}

/** @param {number} bytes @returns {string} GB without trailing zeros: 4, 2.5 */
export function toGb(bytes) {
  return String(Math.round((bytes / GB) * 100) / 100);
}
