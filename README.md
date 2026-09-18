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
 middleware:8000          (prefix routing, static service discovery, CORS)
      |
      +--> lb-get:9000  --> backend-get-1:8080  / backend-get-2:8080
      +--> lb-post:9000 --> backend-post-1:8080 / backend-post-2:8080
      +--> lb-put:9000  --> backend-put-1:8080  / backend-put-2:8080
                                    |
                                    v
                              postgres:5432
```

12 containers, 6 images.

## Modules

| Module | Container(s) | Notes |
|---|---|---|
| Frontend | `frontend` | Static HTML + nginx. Port 3000. |
| Middleware | `middleware` | Reads `routes.json`, matches prefix, proxies to an LB. Port 8000. |
| Load balancer | `lb-get`, `lb-post`, `lb-put` | One image, three configs via `TARGETS`. Round robin + health checks. |
| Backends | `backend-{get,post,put}-{1,2}` | One image, role set by `ROLE`. MVC. |
| Database | `postgres_db` | Seeded from `schema.sql` on first start. |


## Endpoints

| Method | Path | Goes to |
|---|---|---|
| GET | `/instance?id=1` | read service |
| POST | `/instance/create` | write service |
| PUT | `/instance/update-status` | update service |
| GET | `/health` | liveness of whatever tier you asked |
| GET | `/status` | that tier's view of its downstream health |

## Demo

1. Click Read a few times, you'll see how worker IDs alternate.
2. Run `docker stop backend-get-2`.
3. Click Read a few times, you'll see how the worker ID 2 is skipped.
4. Run `docker start backend-get-2` and wait 2 seconds.
5. Click Read a few times, you'll see how worker IDs alternate.
6. `docker stop backend-get-1 backend-get-2`. Clicking Read returns 503.