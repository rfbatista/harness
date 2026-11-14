# k3s resource shapes

Create with the `k3s` MCP resource tools. Every resource is new and named after the task.
Never edit, scale or delete one that existed before.

## Naming

| Thing | Name | Where |
|---|---|---|
| Task database and role | `task_<task short id>` | inside the engine |
| New engine's namespace | `task-<task short id>` | cluster |
| New engine's Deployment, Service, Secret | `task-<task short id>` | that namespace |
| NodePort Service beside an existing engine | `<engine>-nodeport-task-<task short id>` | the engine's namespace |

`<engine>` is `postgres` or `mysql`.

## New engine (only when none runs)

- Namespace `task-<task short id>`.
- Secret `task-<task short id>` holding the superuser password (random).
- Deployment `task-<task short id>`, one replica, the official image (`postgres:16`, `mysql:8`), the Secret as the password environment variable, one container port (5432 or 3306), a label `app: task-<task short id>`. An `emptyDir` volume is enough: these are test data.
- Service `task-<task short id>`, ClusterIP, selecting that label.
- A NodePort Service as below, in the same namespace.

## NodePort Service

- Type `NodePort`, port 5432 or 3306, target the same port.
- `selector`: copy the engine pod's own labels. Same pod as the existing ClusterIP Service.
- Leave `nodePort` unset so the cluster picks a free one; read it back from the created Service.
- The existing Service is never changed.

## Node address

List the nodes and read the `InternalIP` address of the node. The application reaches the engine at `InternalIP:<nodePort>`. Check by running a connection through `pods_exec`, not from the host.

## Checks

- Pod phase `Running` and ready before `pods_exec`.
- The Service lists the pod as an endpoint.
