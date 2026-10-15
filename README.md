# Project Aurelius

## How to run it

### Prerequisites
Make sure you have `docker` installed, if not, download it from the [official website](https://www.docker.com/get-started/) and follow it's installation process. After you have confirmed that docker has correctly been installed, proceed.

> Note: If running docker on linux cli, make sure `docker compose` is also installed

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
      |
      v
 nginx-gateway:8000       (reverse proxy, prefix routing, load balancing, CORS)
      |                   (upstreams rendered from consul by consul-template)
      +--> get_pool  --> backend-get-1:8080  / backend-get-2:8080
      +--> post_pool --> backend-post-1:8080 / backend-post-2:8080
      +--> put_pool  --> backend-put-1:8080  / backend-put-2:8080
                                    |
                                    v
                              postgres:5432

 backends register/deregister ---> consul:8500 <--- nginx-gateway watches
                                   (health checks every backend's /health)
```

10 containers, 5 images.

## Modules

| Module | Container(s) | Notes |
|---|---|---|
| Frontend | `frontend` | Static HTML + nginx. Port 3000. |
| Gateway + load balancer | `nginx-gateway` | nginx + consul-template. Config is `nginx-gateway/templates/nginx.conf.ctmpl`. Matches prefix, load balances to a pool. Port 8000. |
| Service discovery | `consul` | Single node, dev mode. Service registry + health checks. UI on port 8500. |
| Backends | `backend-{get,post,put}-{1,2}` | One image, role set by `ROLE`, identity set by `INSTANCE_ID`. Register themselves in consul. MVC. |
| Database | `postgres_db` | Seeded from `schema.sql` on first start. |

## Service discovery

| Step | What happens |
|---|---|
| Register | On start, each backend registers in consul as service `backend`, tagged with its role (`get`, `post`, `put`), address `INSTANCE_ID:8080`. Retries up to 10 times while consul starts. |
| Health check | Consul calls `http://INSTANCE_ID:8080/health` every 5s (2s timeout). Only passing instances are used. |
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

Consul UI: <http://localhost:8500/ui>.

## Demo

1. Open <http://localhost:8500/ui>, you'll see the 6 backends under `backend`, all passing.
2. Click Read a few times, you'll see how worker IDs alternate.
3. Run `docker stop backend-get-2`. It disappears from consul and nginx in about a second.
4. Click Read a few times, only worker 1 answers.
5. Run `docker start backend-get-2`. It registers again and is back in about 2 seconds.
6. Click Read a few times, you'll see how worker IDs alternate.
7. `docker stop backend-get-1 backend-get-2`. Clicking Read returns 502.
8. See the rendered config with `docker exec nginx-gateway cat /etc/nginx/nginx.conf`.
