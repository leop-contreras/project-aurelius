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
      |
      +--> get_pool  --> backend-get-1:8080  / backend-get-2:8080
      +--> post_pool --> backend-post-1:8080 / backend-post-2:8080
      +--> put_pool  --> backend-put-1:8080  / backend-put-2:8080
                                    |
                                    v
                              postgres:5432
```

9 containers, 4 images.

## Modules

| Module | Container(s) | Notes |
|---|---|---|
| Frontend | `frontend` | Static HTML + nginx. Port 3000. |
| Gateway + load balancer | `nginx-gateway` | nginx, config in `nginx-gateway/nginx.conf`. Matches prefix, load balances to a pool. Port 8000. |
| Backends | `backend-{get,post,put}-{1,2}` | One image, role set by `ROLE`. MVC. |
| Database | `postgres_db` | Seeded from `schema.sql` on first start. |

## Endpoints

| Method | Path | Goes to |
|---|---|---|
| GET | `/instance?id=1` | read service |
| POST | `/instance/create` | write service |
| PUT | `/instance/update-status` | update service |
| GET | `/health` | nginx liveness, answered by nginx |
| GET | `/status` | nginx connection stats |

## Demo

1. Click Read a few times, you'll see how worker IDs alternate.
2. Run `docker stop backend-get-2`.
3. Click Read a few times. The first few that land on worker 2 take abouts 1s (nginx fails to connect and retries on worker 1), after 3 failures worker 2 is skipped.
4. Run `docker start backend-get-2` and wait up to 30 seconds (`fail_timeout`).
5. Click Read a few times, you'll see how worker IDs alternate.
6. `docker stop backend-get-1 backend-get-2`. Clicking Read returns 502.

## Note

nginx resolves the backend IPs once, when it starts. Restarted backends can come back with different IPs, and nginx keeps sending to the old ones, so requests start landing on the wrong service and return 404. 
Run `docker restart nginx-gateway` after restarting backends.
