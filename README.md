# devops-mails

A small Go web app to configure an SMTP relay (postfix-style) and send mail to a
list of target recipients from a browser UI.

Built with [Gin](https://github.com/gin-gonic/gin) (HTTP) and
[go-mail](https://github.com/wneessen/go-mail) (SMTP).

## Run

```bash
go build -o devops-mails .
./devops-mails --addr :8080 --config smtp-config.json
```

Then open <http://localhost:8080>.

| Flag       | Default            | Description                     |
|------------|--------------------|---------------------------------|
| `--addr`   | `:8080`            | Listen address                  |
| `--config` | `smtp-config.json` | Where SMTP settings are stored  |

## Usage

1. **SMTP Config** — set the relay host/port, encryption (none / STARTTLS /
   SSL), optional username & password, and the sender identity. Settings are
   saved to the config JSON file (mode `0600`).
2. **Compose** — paste target addresses (one per line, or comma/semicolon
   separated), write a subject and body (plain text or HTML), and send. The
   result page shows per-recipient delivery status.

## Layout

```
main.go                    entrypoint / flags
internal/config            SMTP settings struct + JSON persistence
internal/mailer            message building + SMTP delivery
internal/server            Gin routes, embedded HTML templates + CSS
```

Templates and CSS are compiled into the binary via `go:embed`, so the single
binary is self-contained.

## Kubernetes / CI

Manifests live in `k8s/` (kustomize) and deploy to namespace `eadn`:

| File                    | Purpose                                                          |
|-------------------------|------------------------------------------------------------------|
| `pvc.yaml`              | Longhorn RWO PVC (`storageClassName: longhorn`) for the config   |
| `deployment.yaml`       | master: 1 replica, `Recreate` (RWO), distroless nonroot, probes  |
| `service.yaml`          | ClusterIP `:80 → :8080` (UI) and `:8671` (agent)                 |
| `ingress.yaml`          | `ingressClassName: nginx`, host `devops-mails.eadn.dz`           |
| `worker-deployment.yaml`| a worker pod running a check and triggering the master           |
| `agent-secret.yaml`     | shared bearer token for the master ↔ worker channel              |
| `kustomization.yaml`    | ties them together; both image tags are patched by CI            |

The config file is written to `/data/smtp-config.json` on the PVC. Because the
PVC is ReadWriteOnce, the Deployment is pinned to **1 replica** with the
**Recreate** strategy (a rolling update would deadlock on Longhorn multi-attach).

### CI (Gitea Actions → ArgoCD)

`.gitea/workflows/build-and-push.yaml` builds and pushes the image with
[`ko`](https://ko.build) (no Docker daemon, no Dockerfile needed — the Go
analog of the Java Jib pipeline), pushing to the in-cluster registry
`gitea.eadn.dz/eadn-factory/devops-mails` over HTTP (`--insecure-registry`).
It then writes the short-SHA tag into `k8s/kustomization.yaml` and commits it
with `[skip ci]`, so **ArgoCD converges** — no `kubectl` from CI.

Required secrets: `CI_PUSH_USER` / `CI_PUSH_TOKEN` (registry push),
`GITEA_TOKEN` (tag commit, needs `contents: write`).

`Dockerfile` and `.ko.yaml` are provided for local/manual `docker build`; the
`.ko.yaml` base image (`distroless:nonroot`, uid 65532) matches the
Deployment's `securityContext`.

The CI builds **two images** — `devops-mails` (master) and
`devops-mails-worker` — and patches both tags into `kustomization.yaml`.

> Edit before first deploy: the ingress host/class, the namespace in
> `kustomization.yaml`, the registry path if your group differs, and the token
> in `agent-secret.yaml`.

## Master / worker agents

The master (this app) exposes an **agent listener on `:8671`** alongside the web
UI. A **worker** (`cmd/worker`, a separate binary/image) runs a check command on
an interval; when the command exits `0` ("true") it POSTs a trigger to the
master, which relays an email through the SMTP config set in the UI. The worker
owns the task (check + recipients + subject/body); the master authenticates with
a shared bearer token (`AGENT_TOKEN`) and sends.

By default a worker is **edge-triggered**: it mails once when a check goes
false→true, not every interval (set `REPEAT=true` to mail on every true check).
Registered workers and recent triggers show on the **Agents** page of the UI.

### Run a worker as a systemd service

```bash
go build -o devops-mails-worker ./cmd/worker
sudo install -m 0755 devops-mails-worker /usr/local/bin/
sudo mkdir -p /etc/devops-mails
sudo install -m 0600 deploy/systemd/agent.env /etc/devops-mails/agent.env   # then edit it
sudo cp deploy/systemd/devops-mails-agent.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now devops-mails-agent
```

All settings are environment variables (see `deploy/systemd/agent.env`):
`MASTER_URL`, `AGENT_TOKEN`, `WORKER_NAME`, `CHECK_COMMAND`, `CHECK_INTERVAL`,
`REPEAT`, `MAIL_TARGETS`, `MAIL_SUBJECT`, `MAIL_BODY`, `MAIL_HTML`.

### Run a worker as a Kubernetes pod

`k8s/worker-deployment.yaml` runs the worker image with the same settings as
env vars. Note a pod only sees **its own** filesystem — for host file checks use
the systemd service (or mount a `hostPath`/shared volume); the pod worker suits
network/HTTP or mounted-volume checks. In-cluster it reaches the master at
`http://devops-mails:8671`. External systemd workers need `:8671` exposed via a
dedicated LoadBalancer/NodePort Service (the HTTP ingress only routes `:80`).

The worker image uses an `alpine` base (not distroless) because checks run via
`sh -c`.
