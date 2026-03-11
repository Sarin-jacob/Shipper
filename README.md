# Shipper

Shipper is a blazing-fast, lightweight, and fully self-hosted CI/CD orchestrator and Docker registry manager. It is designed to be a "pipeline-in-a-box" that automatically detects your build configurations, compiles your images, pushes them to your private registry, and cleans up after itself.

No massive Jenkins instances, no heavy JavaScript SPA frameworks—just a highly efficient Go backend and a lightning-fast Vanilla JS + Tailwind UI.

## Features

* **Zero-Config Detection:** Automatically detects `docker-compose.yml` or `Dockerfile` in your repository and extracts the build context and service names.
* **Smart Versioning:** Automatically applies semantic versioning (e.g., `0.1.0` -> `0.1.1`), commit hashes, and `latest` tags to your builds.
* **Built-in Garbage Collection:** Includes a strict retention policy (keeps `latest` and the highest patch of each minor version). Shipper communicates directly with your registry to untag old images and trigger hard-drive garbage collection.
* **Blazing Fast UI:** Built with Vanilla JS and TailwindCSS. Features real-time dashboard updates, log streaming, and modal-based project management.
* **Background Scheduler:** Automatically polls your Git repositories for new commits (without cloning the whole repo) and triggers builds when updates are detected.
* **Single Binary Deployment:** The Go backend serves the compiled Vite frontend and the REST API from a single, lightweight Alpine Linux Docker container.

---

## Architecture

```text
[ Vanilla JS + Tailwind UI ]  <-- HTTP API -->  [ Shipper Go Backend ]
                                                        |
                                            (Direct Socket Access)
                                                        |
[ Git Repositories ] <-- Clones & Polls -- [ Host Docker Engine (buildx) ]
                                                        |
                                              (Pushes & Cleans up)
                                                        v
                                             [ OCI Private Registry ]

```

---

## Quick Start (Deployment)

Shipper is designed to run alongside an official OCI Docker Registry.

### 1. Prerequisites

* A server with **Docker** and **Docker Compose** installed.
* A domain/subdomain pointing to your server (e.g., `oci.jell0.online`) if you plan to expose the registry publicly.

### 2. Docker Compose

Create a `docker-compose.yml` file:

```yaml
services:
  shipper:
    image: oci.jell0.online/shipper:latest
    build:
      context: .
      dockerfile: Dockerfile
    pull_policy: missing
    container_name: shipper
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - SHIPPER_PORT=8080
      - SHIPPER_REGISTRY=oci.jell0.online
      - SHIPPER_POLL_INTERVAL=1h
      - TZ=Asia/Kolkata
    volumes:
      - ./shipper_data:/app/data
      - /var/run/docker.sock:/var/run/docker.sock # Required for Buildx
    depends_on:
      - registry

  registry:
    image: registry:2
    container_name: shipper_registry
    restart: unless-stopped
    ports:
      - "5000:5000"
    environment:
      - REGISTRY_STORAGE_DELETE_ENABLED=true # Required for Shipper GC
    volumes:
      - ./registry_data:/var/lib/registry
```

### 3. Start the Engine

```bash
docker compose up -d
```

Access the Shipper UI at `http://localhost:8080`.

---

## Configuration Variables

Shipper is highly configurable via environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `SHIPPER_PORT` | `8080` | Port the API and UI run on. |
| `SHIPPER_REGISTRY` | `oci.jell0.online` | The URL of your private registry. |
| `SHIPPER_POLL_INTERVAL` | `1h` | How often to check Git for new commits. |
| `SHIPPER_DATA_DIR` | `./data` | Where SQLite DB and text logs are stored. |
| `SHIPPER_STATIC_DIR` | `./static` | Path to the compiled frontend UI files. |
| `SHIPPER_REGISTRY_CONTAINER` | `shipper_registry` | Name of the registry container (for GC). |

---

## Usage Guide

### Adding a Project

Click **+ Add Project** in the UI. Shipper requires a Git repository URL and a branch name.

**For Private Repositories:**
Since Shipper runs headlessly, you must provide a Personal Access Token (PAT) in the URL to bypass interactive password prompts:

```text
https://<YOUR_TOKEN>@github.com/username/repo.git
```

### Custom Tags

Open the **Settings** modal for any project to assign custom tags. If you add `stable, prod`, the next build will automatically push:

* `oci.jell0.online/app:0.1.2`
* `oci.jell0.online/app:commit-a1b2c3`
* `oci.jell0.online/app:latest`
* `oci.jell0.online/app:stable`
* `oci.jell0.online/app:prod`

### Viewing Logs

Click the **Logs** button on any project to view a historical list of all builds. Click on a specific build to stream the raw `docker buildx` terminal output.

---

## 🛠️ Local Development

If you want to modify Shipper, you can run the backend and frontend separately.

**1. Start the Go Backend:**

```bash
cd backend
go run cmd/shipper/main.go
```

*The backend will run on `localhost:8080` and create a local SQLite database in `./data/shipper.db`.*

**2. Start the Vite Frontend:**

```bash
cd frontend
npm install
npm run dev
```

*The Vite server will run on `localhost:5173` and proxy all `/api` requests to the Go backend.*