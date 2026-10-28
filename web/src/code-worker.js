// The code worker: prepares a diff page (maps it, builds each file's rows,
// highlights them) off the main thread. Built as its own bundle,
// static/code-worker.js, by cmd/webbuild; the code module's preparer.js talks
// to it with the messages diff-prep.js documents.

import { diffPreparer, serve } from "./modules/code/infrastructure/diff-prep.js";
import { hljsHighlighter } from "./modules/code/infrastructure/hljs-highlighter.js";
import { createHljs } from "./highlight.js";

const handle = serve(diffPreparer(hljsHighlighter(createHljs())), (msg) => self.postMessage(msg));
self.onmessage = (e) => handle(e.data);
