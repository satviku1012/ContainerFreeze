# ContainerFreeze

ContainerFreeze is a Linux prototype for checkpointing a process tree with
CRIU and restoring it on the first HTTP request. The proxy keeps each incoming
request open while the daemon restores the process and waits for its TCP
listener to accept connections, then forwards the request to the restored
service.

## Requirements

- Linux with CRIU installed and configured for the host kernel
- Go 1.26 or newer
- `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` to regenerate the RPC code
- `grpcurl` for the checkpoint command below
- Appropriate privileges and kernel support for CRIU, namespaces, and cgroups

CRIU restores the process namespaces and cgroup state captured in the image.
ContainerFreeze passes `--manage-cgroups=soft` to let CRIU restore cgroup data
when possible while tolerating unavailable optional cgroup settings. Run the
daemon with the privileges required by the target process and host configuration.

## Build

```sh
make
```

The proxy listens on `:8081`; the gRPC daemon listens on `:50051`.

## Configure

| Component | Environment variable | Default |
| --- | --- | --- |
| Daemon | `PODFREEZE_GRPC_ADDR` | `:50051` |
| Daemon | `PODFREEZE_CRIU_PATH` | `criu` from `PATH` |
| Daemon | `PODFREEZE_CHECKPOINT_DIR` | `${TMPDIR}/podfreeze-checkpoints` |
| Proxy | `PODFREEZE_GRPC_ADDR` | `127.0.0.1:50051` |
| Proxy | `PODFREEZE_PROXY_ADDR` | `:8081` |

Use durable storage for the checkpoint directory if checkpoints must survive a
daemon restart. Each container ID maps to one directory below that root.

## Checkpoint and wake a process

Start the daemon and proxy on a supported Linux host. Checkpoint a process tree
by calling `CheckpointContainer` with a stable ID and its root PID:

```sh
grpcurl -plaintext -d '{"containerId":"web-1","pid":1234}' \
  localhost:50051 podfreeze.Orchestrator/CheckpointContainer
```

The daemon runs `criu dump` with established TCP connections and cgroup handling
enabled. CRIU terminates the dumped process after a successful checkpoint.
Checkpointing requires the process tree to be in a state CRIU can capture.

Send an HTTP request to the proxy with `X-Container-ID: web-1` and
`X-Target-IP: 127.0.0.1:8080`. The target defaults to `127.0.0.1:8080`; when no
container header is supplied, the first URL path segment is used as the ID.
Requests wait up to 15 seconds for restoration and target readiness. The daemon
coalesces simultaneous wake requests for the same container and marks a
successful restore active until that ID is checkpointed again or the daemon
restarts.

This project is a process-level prototype: it does not create container
namespaces, configure cgroup limits, or discover PIDs and target addresses from
a container runtime. Those must be arranged by the deployment environment.
