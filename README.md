# Project Aurelius

## How to run it

### Prerequisites
Make sure you have `docker` installed, if not, download it from the [official website](https://www.docker.com/get-started/) and follow it's installation process. After you have confirmed that docker has correctly been installed, proceed.

Note: If running docker on linux cli, make sure `docker compose` is also installed

### Linux and MacOS
#### Option 1:
Run the following command at the root of the project:
```bash
./setup.sh
```

#### Option 2:
Run the following command at the root of the project: `docker compose up --build -d`.

### Windows
Run the following command at the root of the project: `docker compose up --build -d`.

Then open <http://localhost:3000>.

## How to kill it
Run `docker compose down -v`.

## Architecture

```
frontend:3000
      |  HTTP/JSON
      v
 nginx-gateway:8000       (reverse proxy, CORS)
      |
      v
 api-service:8080         (REST -> gRPC)
      |  gRPC
      v
 nginx-gateway:9000       (gRPC load balancing, upstreams rendered from consul)
      |
      +--> get_pool  --> backend-get-1:50051  / backend-get-2:50051
      +--> post_pool --> backend-post-1:50051 / backend-post-2:50051 --publish--+
      +--> put_pool  --> backend-put-1:50051  / backend-put-2:50051  --publish--+
                                    |       (put calls get through :9000)    |
                                    v                                        v
                              postgres:5432 <--- events-history-1/2 <--- kafka:9092
                                                 events-audit-1/2   <---  (instances.created,
                                                                           instances.status-updated)

 backends register/deregister ---> consul:8500 <--- nginx-gateway watches
                                   (gRPC health check on every backend)
```

16 containers, 8 images.

## Modules

| Module | Container(s) | Notes |
|---|---|---|
| Frontend | `frontend` | Static HTML + nginx. Port 3000. |
| Gateway + load balancer | `nginx-gateway` | nginx + consul-template. Config is `nginx-gateway/templates/nginx.conf.ctmpl`. Port 8000 (HTTP) and 9000 (gRPC). |
| API service | `api-service` | Receives the frontend's REST calls and calls the backends over gRPC through `nginx-gateway:9000`. MVC. |
| Service discovery | `consul` | Single node, dev mode. Service registry + health checks. UI on port 8500. |
| Backends | `backend-{get,post,put}-{1,2}` | gRPC on port 50051. One image, role set by `ROLE`, identity set by `INSTANCE_ID`. Register themselves in consul. post and put publish events to Kafka. MVC. |
| Message broker | `kafka` | Apache Kafka 3.8, single node, KRaft (no Zookeeper). Only reachable inside the compose network, as `kafka:9092`. |
| Events service | `events-{audit,history}-{1,2}` | Kafka consumers. One image, role set by `ROLE`: `audit` logs every event, `history` writes it to `instance_history`. MVC. |
| Database | `postgres_db` | Seeded from `schema.sql` on first start. |

## gRPC

Contract is `proto/instance.proto`, service `aurelius.InstanceService`:

| RPC | Served by | Request | Returns |
|---|---|---|---|
| `GetInstance` | get backends | `{"id": 1}` | `Instance` |
| `CreateInstance` | post backends | `{"name": "x"}` | `Instance` |
| `UpdateStatus` | put backends | `{"id": 1, "status": "active"}` | `Instance` |

Each backend only registers the RPC of its role. nginx routes by RPC path (`location /aurelius.InstanceService/GetInstance` -> `get_pool`) with `grpc_pass`. The answering worker comes back in the `x-worker-id` response header.

Communication between services: before updating, `UpdateStatus` calls `GetInstance` on the get service through `nginx-gateway:9000` to check the instance exists. If it doesn't, it returns `NotFound` without touching the database.

Generated code lives in `backend/proto_gen` and `api-service/proto_gen`. To regenerate it you need `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc`, then run `make generate_proto_code_grpc`. Without installing anything:

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD:/src" -w /src golang:1.27-alpine sh -c "apk add -q protobuf protobuf-dev make && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && make generate_proto_code_grpc"
```

## Kafka

Writes stay synchronous over gRPC. After the database write succeeds, the backend publishes an event and answers without waiting for Kafka.

| Topic | Published by | Partitions | Key | Payload |
|---|---|---|---|---|
| `instances.created` | post backends, after `CreateInstance` | 3 | instance id | `InstanceEvent` (JSON) |
| `instances.status-updated` | put backends, after `UpdateStatus` | 3 | instance id | `InstanceEvent` (JSON) |

Same key, same partition, so all events of one instance are consumed in order. Reads don't publish anything, and a failed write (e.g. `NotFound`) doesn't either.

| Consumer group | Containers | Does |
|---|---|---|
| `audit-service` | `events-audit-1`, `events-audit-2` | Logs every event (`docker logs events-audit-1`). |
| `history-service` | `events-history-1`, `events-history-2` | Inserts every event in the `instance_history` table. |

- Fan-out: both groups subscribe to both topics, so every event reaches audit and history.
- Queue: inside a group, the 3 partitions of each topic are split between its 2 instances (2 + 1). If one instance stops, the other takes all of them.
- At-least-once: offsets are committed only after the event is handled, with 3 retries. `instance_history.event_id` is `UNIQUE`, so a redelivered event is ignored (`[history] duplicate event ... ignored`). Audit logs it again.
- Topics are created by the events service on start (`EnsureTopic`), with 3 partitions.
- If Kafka is down when a backend publishes, the write still answers OK, the error is logged (`Failed to publish ...`) and the event is lost.
- If Kafka goes down, the consumers stay up and reconnect, waiting 1s, 2s, 4s... up to 30s between attempts (`Consumer stopped: ..., reconnecting in ...`). Events published after Kafka is back are consumed normally.

`InstanceEvent`:

```json
{"event_id": "uuid", "type": "instances.created", "instance_id": 3, "name": "x", "status": "active", "worker_id": "3", "occurred_at": "2026-10-24T00:58:02Z"}
```

`instance_history` is created by `schema.sql`, which only runs on an empty volume. If you ran an older version, start with `docker compose down -v`.

### Inspecting it

In Git Bash prefix these with `MSYS_NO_PATHCONV=1`, otherwise it rewrites `/opt/kafka/bin`.

```bash
# topics and partitions
docker exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe

# which instance holds which partition, offsets and lag
docker exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group audit-service
docker exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group history-service

# raw events, with their key
docker exec kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic instances.created --from-beginning --property print.key=true

# history
docker exec postgres_db psql -U adm1n -d aurelius -c "SELECT instance_id, event_type, status, worker_id, occurred_at FROM instance_history ORDER BY occurred_at"
```

## Service discovery

| Step | What happens |
|---|---|
| Register | On start, each backend registers in consul as service `backend`, tagged with its role (`get`, `post`, `put`), address `INSTANCE_ID:50051`. Retries up to 10 times while consul starts. |
| Health check | Consul runs a gRPC health check (`grpc.health.v1`) on `INSTANCE_ID:50051` every 5s (2s timeout). Only passing instances are used. |
| Deregister | On `docker stop` (SIGTERM) the backend deregisters itself before exiting. If it dies without deregistering, consul removes it after 1 minute critical. |
| Discovery | consul-template watches `get.backend`, `post.backend` and `put.backend`, renders `nginx.conf` and runs `nginx -s reload` on every change. No healthy instances renders a placeholder `server 127.0.0.1:65535 down;` and nginx returns 502. |
| Resolve | `resolver 127.0.0.11` + `resolve` makes nginx resolve backend names through Docker's DNS at runtime, so a backend that died before consul noticed doesn't break a reload. |

## Endpoints

| Method | Path | Goes to |
|---|---|---|
| GET | `/instance?id=1` | read service |
| POST | `/instance/create` | write service |
| PUT | `/instance/update-status` | update service |
| GET | `/health` | nginx liveness, answered by nginx |
| GET | `/status` | nginx connection stats |

gRPC on `localhost:9000`, see [gRPC](#grpc). Consul UI: <http://localhost:8500/ui>.

## grpcurl tests

Install with `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`. Run from the root of the project, in Git Bash on Windows. The server has no reflection, so every call passes the proto file.

Without installing it, replace `grpcurl` with `MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD/proto:/proto" fullstorydev/grpcurl`, `-import-path proto` with `-import-path /proto`, and `localhost:9000` with `host.docker.internal:9000`.

### 1. Describe the service

```bash
grpcurl -import-path proto -proto instance.proto describe aurelius.InstanceService
```
```
aurelius.InstanceService is a service:
service InstanceService {
  rpc CreateInstance ( .aurelius.CreateInstanceRequest ) returns ( .aurelius.Instance );
  rpc GetInstance ( .aurelius.GetInstanceRequest ) returns ( .aurelius.Instance );
  rpc UpdateStatus ( .aurelius.UpdateStatusRequest ) returns ( .aurelius.Instance );
}
```

### 2. GetInstance, showing the worker that answered

```bash
grpcurl -plaintext -v -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
Response headers received:
server: nginx/1.31.6
x-worker-id: 2

Response contents:
{
  "id": 1,
  "name": "Instance 1",
  "status": "active"
}
```

Run it again and `x-worker-id` alternates between 1 and 2.

### 3. CreateInstance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"name": "from-grpcurl"}' localhost:9000 aurelius.InstanceService/CreateInstance
```
```
{
  "id": 5,
  "name": "from-grpcurl",
  "status": "active"
}
```

### 4. UpdateStatus (put service calls get service)

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 2, "status": "active"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
{
  "id": 2,
  "name": "Instance 2",
  "status": "active"
}
```

`docker logs backend-put-1` shows the call to the get service, e.g. `Instance 1 exists, checked by read worker [2]`.

### 5. GetInstance on a missing instance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 6. UpdateStatus on a missing instance

The `NotFound` comes from the get service, the put service never touches the database.

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999, "status": "x"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 7. No healthy backends

```bash
docker stop backend-get-1 backend-get-2
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/GetInstance
docker start backend-get-1 backend-get-2
```
```
ERROR:
  Code: Unavailable
  Message: unexpected HTTP status code received from server: 502 (Bad Gateway); transport: received unexpected content-type "text/html"
```

### 8. Without the proto file

```bash
grpcurl -plaintext localhost:9000 list
```
```
Failed to list services: server does not support the reflection API
```

## Demo

1. Open <http://localhost:8500/ui>, you'll see the 6 backends under `backend`, all passing.
2. Click Read a few times, you'll see how worker IDs alternate.
3. Run `docker stop backend-get-2`. It disappears from consul and nginx in about a second.
4. Click Read a few times, only worker 1 answers.
5. Run `docker start backend-get-2`. It registers again and is back in about 2 seconds.
6. Click Read a few times, you'll see how worker IDs alternate.
7. `docker stop backend-get-1 backend-get-2`. Clicking Read returns 502, and so does Update, because it needs the get service.
8. See the rendered config with `docker exec nginx-gateway cat /etc/nginx/nginx.conf`.
9. Click Create and Update, then run `docker logs events-audit-1` and `docker logs events-audit-2`. Each event shows up in one of them.
10. Query `instance_history` (see [Inspecting it](#inspecting-it)), the same events are there.
11. `docker stop events-history-1 events-history-2`, click Create a few times, `docker start events-history-1 events-history-2`. The history catches up with the events it missed.
