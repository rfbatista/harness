// The top bar's server entry: gohydrate bundles it for goja and renders its
// default export with the island props (the top-bar model) as the one argument.
// No browser or Node globals are used on this path.

import { ServerTopBar } from "../composition/server-root";

export default function TopBar(props: unknown) {
  return <ServerTopBar props={props} />;
}
