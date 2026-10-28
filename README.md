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

`setup.sh` also removes instance containers left from a previous run and pulls `hashicorp/http-echo`. With plain `docker compose up`, the first create pulls the image itself (~3s instead of ~0.3s).

## How to kill it
Instance containers are created by the backends, not by compose, so `docker compose down` doesn't remove them. Run both (works in Git Bash and PowerShell):

```bash
docker rm -f $(docker ps -aq --filter label=aurelius.instance)
docker compose down -v
```

If there are no instances, the first command prints `docker: 'docker rm' requires at least 1 argument`. That's fine.

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

 post / put --- /var/run/docker.sock ---> host docker daemon
                                              |
                                              +--> aurelius-instance-1  localhost:10000
                                              +--> aurelius-instance-2  localhost:10001
                                              +--> ...  (hashicorp/http-echo, max 5)
```

16 containers, 8 images, plus up to 5 instance containers outside compose.

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
| Database | `postgres_db` | Tables created from `schema.sql` on first start. No seed rows. |
| Instances | `aurelius-instance-<id>` | `hashicorp/http-echo`, created by post and stopped/removed by put through the Docker socket. Not part of compose. See [Instances](#instances). |

## gRPC

Contract is `proto/instance.proto`, service `aurelius.InstanceService`:

| RPC | Served by | Request | Returns |
|---|---|---|---|
| `GetInstance` | get backends | `{"id": 1}` | `Instance` |
| `ListInstances` | get backends | `{}` | `InstanceList` |
| `CreateInstance` | post backends | `{"name": "x"}` | `Instance` |
| `UpdateStatus` | put backends | `{"id": 1, "status": "inactive"}` | `Instance` |
| `RenewInstance` | put backends | `{"id": 1}` | `Instance` |

`Instance` is `{id, name, status, port, expires_at, renew_count}`.

Each backend only registers the RPC of its role. nginx routes by RPC path (`location /aurelius.InstanceService/GetInstance` -> `get_pool`) with `grpc_pass`. The answering worker comes back in the `x-worker-id` response header.

Communication between services: before updating, `UpdateStatus` and `RenewInstance` call `GetInstance` on the get service through `nginx-gateway:9000` to check the instance exists. If it doesn't, it returns `NotFound` without touching the database.

Generated code lives in `backend/proto_gen` and `api-service/proto_gen`. To regenerate it you need `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc`, then run `make generate_proto_code_grpc`. Without installing anything:

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD:/src" -w /src golang:1.27-alpine sh -c "apk add -q protobuf protobuf-dev make && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && make generate_proto_code_grpc"
```

## Instances

Every instance is a real container on the host's Docker daemon, next to the compose containers (not inside them). The backends reach the daemon through `/var/run/docker.sock`, mounted in all 6, and call its HTTP API directly (`backend/dockerx`). Inspired by [ctfd-whale](https://github.com/frankli0324/ctfd-whale).

| Action | What happens |
|---|---|
| Create | post checks the limit, inserts the row with the lowest free port in `10000-10100` and `expires_at = now + 1h`, pulls the image if missing, creates and starts `aurelius-instance-<id>` with `-text="Hello from <name>"`. If Docker fails, the container and the row are removed and it returns 500. |
| Inactive | put checks the instance through get, stops and removes the container, then sets `inactive`. If Docker fails it returns 500 and the instance stays `active`. A container that's already gone counts as removed. |
| Active | Only valid if it's already active (no change). An inactive instance can't be reactivated, create a new one. |
| Renew | put sets `expires_at = now + 1h` again and adds 1 to `renew_count`. Only active instances, at most 5 times. |
| Expire | Both put backends check every 10s for active instances past `expires_at`, and do the same as Inactive. |

| Limit | Value | Where |
|---|---|---|
| Max active instances | 5 | `MAX_CONTAINERS` in `docker-compose.yml` |
| Lifetime | 3600s | `models.Timeout` |
| Renewals | 5 | `models.MaxRenews` |
| Ports | 10000-10100, one per active instance | `models.MinPort`/`MaxPort`, `UNIQUE` index on `port` for active rows |

Rejections (`Max container count exceeded`, `Inactive instances cannot be reactivated`, `Inactive instances cannot be renewed`, `Max renewal count exceeded`) return 500. An unknown status returns 400 (`Invalid status`).

```bash
curl -s -X POST localhost:8000/instance/create -H 'Content-Type: application/json' -d '{"name": "demo"}'
```
```
{"id":2,"name":"demo","status":"active","port":10000,"expires_at":"2026-10-28T06:34:52Z","renew_count":0}
```

```bash
curl -s localhost:10000
```
```
Hello from demo
```

```bash
curl -s -X PUT localhost:8000/instance/renew -H 'Content-Type: application/json' -d '{"id": 2}'
```
```
{"id":2,"name":"demo","status":"active","port":10000,"expires_at":"2026-10-28T06:34:53Z","renew_count":1}
```

```bash
docker ps --filter label=aurelius.instance --format '{{.Names}} {{.Ports}}'
```
```
aurelius-instance-2 0.0.0.0:10000->5678/tcp
```

- The ports are published on `0.0.0.0`, so instances are reachable from your local network.
- Containers and rows are not reconciled: a container removed by hand leaves its row `active` until it expires or is set inactive.
- Expiring and renewing don't publish Kafka events.

## Kafka

Writes stay synchronous over gRPC. After the database write succeeds, the backend publishes an event and answers without waiting for Kafka.

| Topic | Published by | Partitions | Key | Payload |
|---|---|---|---|---|
| `instances.created` | post backends, after `CreateInstance` and its container is running | 3 | instance id | `InstanceEvent` (JSON) |
| `instances.status-updated` | put backends, after `UpdateStatus` set an instance inactive | 3 | instance id | `InstanceEvent` (JSON) |

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
| GET | `/instances` | read service |
| POST | `/instance/create` `{"name"}` | write service |
| PUT | `/instance/update-status` `{"id", "status"}` | update service |
| PUT | `/instance/renew` `{"id"}` | update service |
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
  rpc ListInstances ( .aurelius.ListInstancesRequest ) returns ( .aurelius.InstanceList );
  rpc RenewInstance ( .aurelius.RenewInstanceRequest ) returns ( .aurelius.Instance );
  rpc UpdateStatus ( .aurelius.UpdateStatusRequest ) returns ( .aurelius.Instance );
}
```

### 2. CreateInstance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"name": "from-grpcurl"}' localhost:9000 aurelius.InstanceService/CreateInstance
```
```
{
  "id": 1,
  "name": "from-grpcurl",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-28T06:34:35Z"
}
```

`curl -s localhost:10000` answers `Hello from from-grpcurl`. grpcurl hides fields with their zero value, like `renewCount: 0`.

### 3. GetInstance, showing the worker that answered

```bash
grpcurl -plaintext -v -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
Response headers received:
content-type: application/grpc
date: Wed, 28 Oct 2026 05:34:36 GMT
server: nginx/1.31.6
x-worker-id: 1

Response contents:
{
  "id": 1,
  "name": "from-grpcurl",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-28T06:34:35Z"
}
```

Run it again and `x-worker-id` alternates between 1 and 2.

### 4. ListInstances

```bash
grpcurl -plaintext -import-path proto -proto instance.proto localhost:9000 aurelius.InstanceService/ListInstances
```
```
{
  "instances": [
    {
      "id": 1,
      "name": "from-grpcurl",
      "status": "active",
      "port": 10000,
      "expiresAt": "2026-10-28T06:34:35Z"
    }
  ]
}
```

### 5. RenewInstance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/RenewInstance
```
```
{
  "id": 1,
  "name": "from-grpcurl",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-28T06:34:38Z",
  "renewCount": 1
}
```

### 6. UpdateStatus to inactive (put calls get, then removes the container)

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1, "status": "inactive"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
{
  "id": 1,
  "name": "from-grpcurl",
  "status": "inactive",
  "port": 10000,
  "expiresAt": "2026-10-28T06:34:38Z",
  "renewCount": 1
}
```

`docker logs backend-put-1` shows `Instance 1 exists, checked by read worker [2]` and `Instance 1 inactive, container removed`. `aurelius-instance-1` is gone from `docker ps -a`.

### 7. Reactivating or renewing an inactive instance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1, "status": "active"}' localhost:9000 aurelius.InstanceService/UpdateStatus
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/RenewInstance
```
```
ERROR:
  Code: FailedPrecondition
  Message: Inactive instances cannot be reactivated
ERROR:
  Code: FailedPrecondition
  Message: Inactive instances cannot be renewed
```

### 8. Unknown status

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1, "status": "x"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
ERROR:
  Code: InvalidArgument
  Message: Invalid status
```

### 9. GetInstance on a missing instance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 10. UpdateStatus on a missing instance

The `NotFound` comes from the get service, the put service never touches the database or Docker.

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999, "status": "inactive"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 11. No healthy backends

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

### 12. Without the proto file

```bash
grpcurl -plaintext localhost:9000 list
```
```
Failed to list services: server does not support the reflection API
```

## Demo

1. Open <http://localhost:8500/ui>, you'll see the 6 backends under `backend`, all passing.
2. In <http://localhost:3000>, click Create. The instance shows up in the Instances table with its link (`http://localhost:10000`) and the time left. It takes ~0.3s, or ~3s the first time if the image wasn't pulled.
3. Open the link, it answers `Hello from instance1`. `docker ps --filter label=aurelius.instance` shows `aurelius-instance-1`.
4. Click Read a few times, you'll see how worker IDs alternate.
5. Run `docker stop backend-get-2`. It disappears from consul and nginx in about a second.
6. Click Read a few times, only worker 1 answers.
7. Run `docker start backend-get-2`. It registers again and is back in about 2 seconds.
8. `docker stop backend-get-1 backend-get-2`. Clicking Read returns 502, and so does Update, because it needs the get service. Start them again.
9. See the rendered config with `docker exec nginx-gateway cat /etc/nginx/nginx.conf`.
10. Click Renew on the instance. `renews` goes to 1/5 and the time left goes back to 60m.
11. Set Update status to `inactive` for id 1 and click Update. The container is stopped and removed (~0.3s), the link and the Renew button disappear.
12. Set it to `active` again, it returns 500 `Inactive instances cannot be reactivated`.
13. Click Create 6 times. The 6th returns 500 `Max container count exceeded`.
14. Make one expire without waiting an hour, e.g. id 2:
    `docker exec postgres_db psql -U adm1n -d aurelius -c "UPDATE instances SET expires_at = now() WHERE id = 2"`. Within 10s (4-7s measured) its container is removed and it shows `inactive`. `docker logs backend-put-1` shows `Instance 2 expired, container removed`.
15. Run `docker logs events-audit-1` and `docker logs events-audit-2`. Each create and each inactive shows up in one of them.
16. Query `instance_history` (see [Inspecting it](#inspecting-it)), the same events are there.
17. `docker stop events-history-1 events-history-2`, set ids 3 and 4 inactive, `docker start events-history-1 events-history-2`. The history catches up with the events it missed.
