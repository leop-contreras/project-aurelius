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
frontend:3000            (challenges and admin pages)
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
| Frontend | `frontend` | Static HTML + nginx, two pages: challenges (`/`) and admin (`/admin.html`). Port 3000. See [Frontend](#frontend). |
| Gateway + load balancer | `nginx-gateway` | nginx + consul-template. Config is `nginx-gateway/templates/nginx.conf.ctmpl`. Port 8000 (HTTP) and 9000 (gRPC). |
| API service | `api-service` | Receives the frontend's REST calls and calls the backends over gRPC through `nginx-gateway:9000`. MVC. |
| Service discovery | `consul` | Single node, dev mode. Service registry + health checks. UI on port 8500. |
| Backends | `backend-{get,post,put}-{1,2}` | gRPC on port 50051. One image, role set by `ROLE`, identity set by `INSTANCE_ID`. Register themselves in consul. post and put publish events to Kafka. MVC. |
| Message broker | `kafka` | Apache Kafka 3.8, single node, KRaft (no Zookeeper). Only reachable inside the compose network, as `kafka:9092`. |
| Events service | `events-{audit,history}-{1,2}` | Kafka consumers. One image, role set by `ROLE`: `audit` logs every event, `history` writes it to `instance_history`. MVC. |
| Database | `postgres_db` | Tables created from `schema.sql` on first start. Seeds 6 challenges, no instances. |
| Instances | `aurelius-instance-<id>` | `hashicorp/http-echo`, created by post and stopped/removed by put through the Docker socket. Not part of compose. See [Instances](#instances). |

## gRPC

Contract is `proto/instance.proto`, service `aurelius.InstanceService`:

| RPC | Served by | Request | Returns |
|---|---|---|---|
| `GetInstance` | get backends | `{"id": 1}` | `Instance` |
| `ListInstances` | get backends | `{}` | `InstanceList` |
| `ListChallenges` | get backends | `{}` | `ChallengeList` |
| `CreateInstance` | post backends | `{"challenge_id": 1}` | `Instance` |
| `UpdateStatus` | put backends | `{"id": 1, "status": "inactive"}` | `Instance` |
| `RenewInstance` | put backends | `{"id": 1}` | `Instance` |

`Instance` is `{id, name, status, port, expires_at, renew_count, challenge_id}`. `Challenge` is `{id, name, description}`.

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
| Create | post looks up the challenge (404 `Challenge not found` if it doesn't exist), checks the limit, inserts the row with the challenge's name, the lowest free port in `10000-10100` and `expires_at = now + 1h`, pulls the image if missing, creates and starts `aurelius-instance-<id>` with `-text="Hello from <name>"`. If Docker fails, the container and the row are removed and it returns 500. |
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
curl -s -X POST localhost:8000/instance/create -H 'Content-Type: application/json' -d '{"challenge_id": 2}'
```
```
{"id":2,"name":"Port Hopper","status":"active","port":10000,"expires_at":"2026-10-30T03:05:13Z","renew_count":0,"challenge_id":2}
```

```bash
curl -s localhost:10000
```
```
Hello from Port Hopper
```

```bash
curl -s -X PUT localhost:8000/instance/renew -H 'Content-Type: application/json' -d '{"id": 2}'
```
```
{"id":2,"name":"Port Hopper","status":"active","port":10000,"expires_at":"2026-10-30T03:05:14Z","renew_count":1,"challenge_id":2}
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

## Frontend

Static pages in `frontend/` (`index.html`, `admin.html`, shared `app.js`), styled like CTFd + ctfd-whale. Tailwind v4 (browser build), lucide icons and Google Fonts load from CDNs, so the browser needs internet.

| Page | What it does |
|---|---|
| Challenges (`/`) | One card per challenge from `GET /challenges`. Clicking one opens a modal with the description and the challenge's instance: Launch, or the id, link, port, time left, renews, Renew and Destroy (= set inactive). |
| Admin (`/admin.html`) | Every instance (All / Active / Inactive) with link, port, time left, renews, and Renew/Destroy per row. Gateway status (`/status`) and the workers that answered the last 20 requests. |

- There's no login. Each challenge has at most one active instance, shared by everyone. That limit is only checked in the page (Launch is hidden while one is active), the API doesn't enforce it.
- After every action a toast shows the worker that answered (`X-Worker-ID`), the request and its status code. Errors show `?`, because the backends only send the worker id on success.
- Errors are shown as the API returned them, in the modal or under the admin table.
- Both pages poll every 2s. The countdowns update every second.

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
| GET | `/challenges` | read service |
| POST | `/instance/create` `{"challenge_id"}` | write service |
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
  rpc ListChallenges ( .aurelius.ListChallengesRequest ) returns ( .aurelius.ChallengeList );
  rpc ListInstances ( .aurelius.ListInstancesRequest ) returns ( .aurelius.InstanceList );
  rpc RenewInstance ( .aurelius.RenewInstanceRequest ) returns ( .aurelius.Instance );
  rpc UpdateStatus ( .aurelius.UpdateStatusRequest ) returns ( .aurelius.Instance );
}
```

### 2. ListChallenges

```bash
grpcurl -plaintext -import-path proto -proto instance.proto localhost:9000 aurelius.InstanceService/ListChallenges
```
```
{
  "challenges": [
    {
      "id": 1,
      "name": "Echo Chamber",
      "description": "The server only repeats what it was told. Launch it and see what it says."
    },
    {
      "id": 2,
      "name": "Port Hopper",
      "description": "Every instance gets its own port. Find yours and connect to it."
    },
    ...
  ]
}
```

Cut after 2 of the 6 challenges.

### 3. CreateInstance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"challenge_id": 1}' localhost:9000 aurelius.InstanceService/CreateInstance
```
```
{
  "id": 1,
  "name": "Echo Chamber",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-30T03:04:55Z",
  "challengeId": 1
}
```

`curl -s localhost:10000` answers `Hello from Echo Chamber`. grpcurl hides fields with their zero value, like `renewCount: 0`.

### 4. GetInstance, showing the worker that answered

```bash
grpcurl -plaintext -v -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
Response headers received:
content-type: application/grpc
date: Fri, 30 Oct 2026 02:04:57 GMT
server: nginx/1.31.6
x-worker-id: 2

Response contents:
{
  "id": 1,
  "name": "Echo Chamber",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-30T03:04:55Z",
  "challengeId": 1
}
```

Run it again and `x-worker-id` alternates between 1 and 2.

### 5. ListInstances

```bash
grpcurl -plaintext -import-path proto -proto instance.proto localhost:9000 aurelius.InstanceService/ListInstances
```
```
{
  "instances": [
    {
      "id": 1,
      "name": "Echo Chamber",
      "status": "active",
      "port": 10000,
      "expiresAt": "2026-10-30T03:04:55Z",
      "challengeId": 1
    }
  ]
}
```

### 6. RenewInstance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1}' localhost:9000 aurelius.InstanceService/RenewInstance
```
```
{
  "id": 1,
  "name": "Echo Chamber",
  "status": "active",
  "port": 10000,
  "expiresAt": "2026-10-30T03:04:58Z",
  "renewCount": 1,
  "challengeId": 1
}
```

### 7. UpdateStatus to inactive (put calls get, then removes the container)

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1, "status": "inactive"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
{
  "id": 1,
  "name": "Echo Chamber",
  "status": "inactive",
  "port": 10000,
  "expiresAt": "2026-10-30T03:04:58Z",
  "renewCount": 1,
  "challengeId": 1
}
```

The put backend that answered logs `Instance 1 exists, checked by read worker [2]` and `Instance 1 inactive, container removed` (`docker logs backend-put-2`). `aurelius-instance-1` is gone from `docker ps -a`.

### 8. Reactivating or renewing an inactive instance

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

### 9. Unknown status

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 1, "status": "x"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
ERROR:
  Code: InvalidArgument
  Message: Invalid status
```

### 10. CreateInstance for a missing challenge

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"challenge_id": 99}' localhost:9000 aurelius.InstanceService/CreateInstance
```
```
ERROR:
  Code: NotFound
  Message: Challenge not found
```

### 11. GetInstance on a missing instance

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999}' localhost:9000 aurelius.InstanceService/GetInstance
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 12. UpdateStatus on a missing instance

The `NotFound` comes from the get service, the put service never touches the database or Docker.

```bash
grpcurl -plaintext -import-path proto -proto instance.proto -d '{"id": 999999, "status": "inactive"}' localhost:9000 aurelius.InstanceService/UpdateStatus
```
```
ERROR:
  Code: NotFound
  Message: Instance not found
```

### 13. No healthy backends

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

### 14. Without the proto file

```bash
grpcurl -plaintext localhost:9000 list
```
```
Failed to list services: server does not support the reflection API
```

## Demo

1. Open <http://localhost:8500/ui>, you'll see the 6 backends under `backend`, all passing.
2. Open <http://localhost:3000>. There are 6 challenge cards. Click Echo Chamber, then Launch an instance. The modal shows the id, the link (`http://localhost:10000`), the port, the time left and `0/5` renews. It takes ~0.3s, or ~3s the first time if the image wasn't pulled. A toast shows the post worker that answered (3 or 4).
3. Open the link, it answers `Hello from Echo Chamber`. `docker ps --filter label=aurelius.instance` shows `aurelius-instance-1`.
4. Open Admin. The instance is there, and Workers that answered alternates `1 2 1 2`: the table is polled every 2s through the get pool.
5. Run `docker stop backend-get-2`. It disappears from consul and nginx in about a second, and only worker 1 answers in Admin.
6. Run `docker start backend-get-2`. It registers again and is back in about 2 seconds.
7. `docker stop backend-get-1 backend-get-2` and reload Challenges: `Could not load the challenges.` Start them again and the cards are back in ~1.5s.
8. See the rendered config with `docker exec nginx-gateway cat /etc/nginx/nginx.conf`.
9. In the challenge's modal click Renew this instance. Renews goes to 1/5 and the time left goes back to 60m.
10. Click Destroy this instance. The container is stopped and removed (~0.3s) and the modal goes back to Launch an instance.
11. Launch instances from 5 different challenges. The 6th shows `500 Max container count exceeded` in the modal and a red toast.
12. Make one expire without waiting an hour, e.g. id 2:
    `docker exec postgres_db psql -U adm1n -d aurelius -c "UPDATE instances SET expires_at = now() WHERE id = 2"`. Within ~12s (10s cleanup tick + 2s poll; 9.7s measured) the open modal goes back to Launch and Admin shows it `inactive`. `docker logs backend-put-1` shows `Instance 2 expired, container removed`.
13. Run `docker stop api-service`. Every request takes 1s (504). An open Admin keeps showing the last list it got; reloading it shows `No instances.`. `docker start api-service` and the table is back on the next poll.
14. Run `docker logs events-audit-1` and `docker logs events-audit-2`. Each create and each destroy shows up in one of them.
15. Query `instance_history` (see [Inspecting it](#inspecting-it)), the same events are there.
16. `docker stop events-history-1 events-history-2`, destroy two instances, `docker start events-history-1 events-history-2`. The history catches up with the events it missed.
