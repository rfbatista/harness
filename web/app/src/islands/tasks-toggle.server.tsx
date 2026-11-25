// The header's Tasks button: gohydrate renders its default export on the server with the island props as the one argument.

import { ServerTasksToggle } from "../composition/server-root";

export default function TasksToggle(props: unknown) {
  return <ServerTasksToggle props={props} />;
}
