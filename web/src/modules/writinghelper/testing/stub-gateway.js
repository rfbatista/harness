// A StepGateway the test (or the dev page) scripts: each step answers from
// its script, a draft streams its words as deltas, and `gate()` holds the next
// step until the test lets it go. Aborting the signal rejects like fetch does.

import { StructuredError } from "../../../shared/domain/errors.js";

export const QUESTIONS = [
  { id: "q1", text: "Which fields get the pen first?", why: "Your text says titles; the draft should name them.", chips: ["Task title and description", "Every field"] },
  { id: "q2", text: "How will you know it is done?", why: "No done criteria yet.", chips: ["A person drafts in a minute"] },
];

/**
 * @param {{
 *   analyze?: object | ((request: object) => object),
 *   draft?: string | ((request: object) => string),
 *   titles?: string[] | ((request: object) => string[]),
 * }} [script]
 */
export function stubGateway(script = {}) {
  /** Every request the gateway got, oldest first. */
  const calls = [];
  /** @type {{ release: () => void } | null} */
  let held = null;
  /** @type {StructuredError | null} */
  let failure = null;

  const resolve = (value, request) => (typeof value === "function" ? value(request) : value);

  const gateway = {
    calls,
    /** The next step waits until the returned release() is called. */
    gate() {
      let release;
      const wait = new Promise((r) => (release = r));
      held = { release, wait };
      return release;
    },
    /** The next step fails with this error. */
    failNext(code, message = "The helper failed.", details) {
      failure = new StructuredError(code, message, undefined, details);
    },
    async runStep(request, { signal, onDelta } = {}) {
      calls.push(request);
      const abort = new Promise((_, reject) => {
        const fail = () => reject(new DOMException("aborted", "AbortError"));
        if (signal?.aborted) fail();
        signal?.addEventListener("abort", fail);
      });
      const work = (async () => {
        if (held) {
          const { wait } = /** @type {any} */ (held);
          held = null;
          await wait;
        }
        if (failure) {
          const err = failure;
          failure = null;
          throw err;
        }
        if (request.step === "analyze") {
          return resolve(script.analyze, request) ?? { already_good: false, questions: QUESTIONS };
        }
        if (request.step === "titles") {
          const base = request.exclude?.length ?? 0;
          return { titles: resolve(script.titles, request) ?? [1, 2, 3].map((n) => `Title option ${base + n}`) };
        }
        const text = resolve(script.draft, request) ?? "A clear brief. Done when: [open: what done looks like]";
        for (const word of text.match(/\S+\s*/g) ?? []) onDelta?.(word);
        return { text };
      })();
      return Promise.race([work, abort]);
    },
  };
  return gateway;
}

