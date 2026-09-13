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

| File               | Purpose                                                          |
|--------------------|------------------------------------------------------------------|
| `pvc.yaml`         | Longhorn RWO PVC (`storageClassName: longhorn`) for the config   |
| `deployment.yaml`  | 1 replica, `Recreate` strategy (RWO), distroless nonroot, probes |
| `service.yaml`     | ClusterIP `:80 → :8080`                                          |
| `ingress.yaml`     | `ingressClassName: nginx`, host `devops-mails.eadn.dz`           |
| `kustomization.yaml` | Ties them together; image tag is patched by CI                 |

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

> Edit before first deploy: the ingress host/class, the namespace in
> `kustomization.yaml`, and the registry path if your group differs.
# devoos-mails
