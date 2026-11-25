// The client entry: the browser's composition root for every island on the page.
// Loaded once per document, after the Alpine bundle.

import { mountIslands } from "./composition/browser-root";

mountIslands(document);
