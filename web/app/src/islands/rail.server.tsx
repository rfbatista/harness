// The rail's server entry: gohydrate bundles it for goja and renders its default
// export with the island props (the rail seed and navigation) as the one
// argument. No browser or Node globals are used on this path.

import { ServerRail } from "../composition/server-root";

export default function Rail(props: unknown) {
  return <ServerRail props={props} />;
}
