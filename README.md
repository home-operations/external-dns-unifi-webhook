# ExternalDNS Webhook Provider for UniFi

[![CI](https://github.com/home-operations/external-dns-unifi-webhook/actions/workflows/ci.yaml/badge.svg)](https://github.com/home-operations/external-dns-unifi-webhook/actions/workflows/ci.yaml)
[![Release](https://img.shields.io/github/v/release/home-operations/external-dns-unifi-webhook)](https://github.com/home-operations/external-dns-unifi-webhook/releases)
[![License](https://img.shields.io/github/license/home-operations/external-dns-unifi-webhook)](LICENSE)
[![Discord](https://img.shields.io/discord/673534664354430999?label=discord&logo=discord&logoColor=white&color=blue)](https://discord.gg/home-operations)

A webhook provider for [ExternalDNS](https://github.com/kubernetes-sigs/external-dns) that manages DNS records in a UniFi Network controller. ExternalDNS keeps DNS in sync with your Kubernetes Ingresses and Services; this provider applies those records to UniFi's built-in DNS via the Network [Integration API](https://developer.ui.com/network/).

## Requirements

| Component     | Minimum version |
| ------------- | --------------- |
| ExternalDNS   | v0.21.0         |
| UniFi OS      | 5.x             |
| UniFi Network | 10.3.58         |

## How it works

The provider runs as a sidecar alongside the ExternalDNS controller. It speaks the ExternalDNS webhook protocol on one side and the UniFi Network Integration API (`/proxy/network/integration/v1/...`, specifically the **DNS Policies** endpoints) on the other. It reaches UniFi one of two ways:

- **Local** (default) — connects directly to the controller on your network.
- **Cloud connector** — proxies through `api.ui.com` for consoles you can't reach on the LAN. See [Cloud connector](#cloud-connector).

Domain filtering is handled by the ExternalDNS controller, not by this webhook — see [Domain filtering](#domain-filtering).

## Limitations

UniFi uses [dnsmasq](https://dnsmasq.org) as its DNS backend, so the provider inherits its constraints:

- **Wildcards** (`*.example.com`) are not supported.
- **One CNAME per name.** The webhook reconciles this transparently:
  - creating a CNAME where one already exists evicts the existing record first;
  - if ExternalDNS sends multiple targets for a single CNAME, only the first is used and the rest are dropped with a warning.

## Quick start

### 1. Create a UniFi API key

Every request authenticates with an API key; username/password auth is not supported.

**Local controller** — log into your console by IP, then go to **Settings → Control Plane → Integrations → Create API Key** and copy the key.

> Only Super Admins can _create_ API keys, but a key keeps working after the user is downgraded. For least privilege, create a dedicated `external-dns` user, generate its key while it's a Super Admin, then drop it to **Site Admin** — that's enough to manage DNS records.

**Cloud connector** — create an **account-level** key in the [UniFi Site Manager](https://unifi.ui.com) under account settings → API. This is different from a per-console local key.

### 2. Store the key in a Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: unifi-dns-secret
stringData:
  UNIFI_API_KEY: <your-api-key>
```

### 3. Install with Helm

Add the ExternalDNS chart repository:

```sh
helm repo add external-dns https://kubernetes-sigs.github.io/external-dns/
```

Create a values file (`external-dns-unifi-values.yaml`):

```yaml
fullnameOverride: external-dns-unifi
provider:
  name: webhook
  webhook:
    image:
      repository: ghcr.io/home-operations/external-dns-unifi-webhook
      tag: main # replace with a versioned release tag
    env:
      - name: UNIFI_HOST
        value: https://unifi.internal # your UniFi controller, or https://api.ui.com for the cloud connector
      - name: UNIFI_API_KEY
        valueFrom:
          secretKeyRef:
            name: unifi-dns-secret
            key: UNIFI_API_KEY
    livenessProbe:
      httpGet:
        path: /healthz
        port: http-webhook
      initialDelaySeconds: 10
      timeoutSeconds: 5
    readinessProbe:
      httpGet:
        path: /readyz
        port: http-webhook
      initialDelaySeconds: 10
      timeoutSeconds: 5
triggerLoopOnEvent: true
policy: sync
sources:
  - gateway-httproute
  - service
txtOwnerId: main
txtPrefix: k8s.main.%{record_type}-
domainFilters:
  - example.com # replace with your domain
serviceMonitor:
  enabled: true
```

Install:

```sh
helm install external-dns-unifi external-dns/external-dns \
  -f external-dns-unifi-values.yaml --version 1.21.1 -n external-dns
```

See the [chart values](https://github.com/kubernetes-sigs/external-dns/blob/master/charts/external-dns/values.yaml) for additional options.

## Configuration

### UniFi connection

| Variable                    | Description                                                                           | Default   |
| --------------------------- | ------------------------------------------------------------------------------------- | --------- |
| `UNIFI_HOST`                | Controller address, or `https://api.ui.com` for the cloud connector (**required**).   | —         |
| `UNIFI_API_KEY`             | API key used to authenticate (**required**). Local and cloud keys differ — see below. | —         |
| `UNIFI_SITE`                | Site name (e.g. `default`) or site UUID. Resolved to the API's UUID at startup.       | `default` |
| `UNIFI_CONSOLE_ID`          | Console ID. Setting this routes requests through the `api.ui.com` cloud connector.    | —         |
| `UNIFI_SKIP_TLS_VERIFY`     | Skip TLS verification. Ignored in cloud mode.                                         | `true`    |
| `UNIFI_CA_CERT`             | Path to a PEM bundle of extra trusted CAs (alternative to skipping verification).     | —         |
| `UNIFI_APPLY_WORKERS`       | Maximum concurrent record operations during a reconcile.                              | `5`       |
| `UNIFI_RETRY_ATTEMPTS`      | Total attempts per request (including the first).                                     | `3`       |
| `UNIFI_RETRY_INITIAL_DELAY` | Initial backoff before the first retry.                                               | `500ms`   |
| `UNIFI_RETRY_MAX_DELAY`     | Maximum backoff between retries (also caps `Retry-After`).                            | `10s`     |
| `UNIFI_REQUEST_TIMEOUT`     | Per-attempt timeout for controller requests. Unset disables it (see below).           | —         |

#### Request timeouts

ExternalDNS gives each call to the webhook a fixed budget: `--webhook-provider-read-timeout` plus `--webhook-provider-write-timeout` (5s + 10s by default). If the webhook has not answered by then, ExternalDNS treats it as a fatal error and exits; an HTTP 5xx from the webhook is only logged as a soft error. Without a per-attempt timeout, a controller that accepts the connection and then stalls keeps the webhook waiting past that budget, and ExternalDNS restarts instead of retrying on the next sync.

Set `UNIFI_REQUEST_TIMEOUT` to bound each attempt. An attempt that times out before the controller answers is retried like any other network error (for `GET` and `DELETE`); a stall after the response headers fails without a retry. Either way, once the request gives up the webhook answers `500`. Size it so `UNIFI_RETRY_ATTEMPTS × UNIFI_REQUEST_TIMEOUT` plus the backoff between attempts stays below the ExternalDNS budget. With the defaults (3 attempts, 500ms initial delay, up to 50% jitter) a `4s` timeout gives a worst case of about 14.3s, under the 15s default. `/records` fetches pages of up to 200 records sequentially, each with its own attempts, so with more than 200 records the worst case adds up across pages. Alternatively, raise the ExternalDNS timeouts; keep their sum below `SERVER_WRITE_TIMEOUT`.

#### Cloud connector

To manage a remote console without exposing the controller on your LAN, set `UNIFI_CONSOLE_ID` and point `UNIFI_HOST` at `https://api.ui.com`. Requests are then proxied through `/v1/connector/consoles/{consoleId}/proxy/network/integration/v1/...`. The Integration API surface is identical to a local connection — only the routing prefix changes.

- Use an **account-level** API key from the [UniFi Site Manager](https://unifi.ui.com), not a per-console local key.
- Find your console ID via the Site Manager API (`GET https://api.ui.com/v1/hosts`) or the console URL in [unifi.ui.com](https://unifi.ui.com).
- `UNIFI_SKIP_TLS_VERIFY` is ignored — `api.ui.com` presents a publicly-trusted certificate, and skipping verification would expose your API key.
- Reconciliation depends on Ubiquiti's API availability and rate limits. The built-in backoff handles `429`s, but a local connection is preferable when reachable.

#### Domain filtering

Configure `--domain-filter` (and its variants) on the **ExternalDNS controller**, not on this webhook. UniFi has no zone concept the webhook could narrow against, so it follows the [ExternalDNS `GetDomainFilter` contract](https://github.com/kubernetes-sigs/external-dns/blob/v0.22.0/docs/contributing/sources-and-providers.md#implementing-getdomainfilter) and leaves filtering to the controller.

### Webhook server

| Variable                     | Description                                                | Default           |
| ---------------------------- | ---------------------------------------------------------- | ----------------- |
| `SERVER_HOST`                | Webhook server bind address.                               | `localhost`       |
| `SERVER_PORT`                | Webhook server port.                                       | `8888`            |
| `SERVER_READ_TIMEOUT`        | Request read timeout.                                      | `60s`             |
| `SERVER_READ_HEADER_TIMEOUT` | Read-header timeout (Slowloris mitigation).                | `5s`              |
| `SERVER_WRITE_TIMEOUT`       | Response write timeout.                                    | `60s`             |
| `SERVER_IDLE_TIMEOUT`        | Keep-alive idle timeout.                                   | `120s`            |
| `SERVER_MAX_HEADER_BYTES`    | Maximum request header size.                               | `65536`           |
| `SERVER_MAX_BODY_BYTES`      | Maximum POST body size before returning `413`.             | `5242880` (5 MiB) |
| `HEALTH_SERVER_ADDR`         | Address for the `/metrics`, `/healthz`, `/readyz` server.  | `:8080`           |
| `READINESS_CACHE_TTL`        | How long `/readyz` caches the upstream probe result.       | `30s`             |
| `PPROF_ENABLED`              | Mount `/debug/pprof/*` on the health server (not in prod). | `false`           |
| `LOG_LEVEL`                  | Log verbosity: `debug`, `info`, `warn`, `error`.           | `info`            |
| `LOG_FORMAT`                 | Set to `text` for human-readable output instead of JSON.   | JSON              |

### Endpoints

| Endpoint        | Port           | Purpose                                                     |
| --------------- | -------------- | ----------------------------------------------------------- |
| `/`             | `8888`         | ExternalDNS negotiate (returns the provider media type).    |
| `/records`      | `8888`         | ExternalDNS `GET` (list) and `POST` (apply changes).        |
| `/healthz`      | `8888`, `8080` | Liveness — `200 OK` while the process is running.           |
| `/readyz`       | `8888`, `8080` | Readiness — probes UniFi, cached for `READINESS_CACHE_TTL`. |
| `/metrics`      | `8080`         | Prometheus metrics.                                         |
| `/debug/pprof/` | `8080`         | Go pprof endpoints (only when `PPROF_ENABLED=true`).        |

`/healthz` and `/readyz` are served on both ports so Kubernetes probes can target the webhook port directly without exposing a second container port through the chart.

## Upgrading

Migrating to the UniFi Network 10.3.58 [Integration API](https://developer.ui.com/network/) introduced breaking changes:

| Setting                                                                                            | Change                                                                                                                                                                                  |
| -------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `UNIFI_EXTERNAL_CONTROLLER`                                                                        | **Removed.** Point `UNIFI_HOST` at the controller directly, or use the [cloud connector](#cloud-connector) for remote consoles.                                                         |
| `DOMAIN_FILTER`, `EXCLUDE_DOMAIN_FILTER`, `REGEXP_DOMAIN_FILTER`, `REGEXP_DOMAIN_FILTER_EXCLUSION` | **Removed.** Configure `--domain-filter` on the ExternalDNS controller instead (see [Domain filtering](#domain-filtering)).                                                             |
| `LOG_FORMAT=test`                                                                                  | **Renamed** to `LOG_FORMAT=text`.                                                                                                                                                       |
| API endpoint                                                                                       | Moved from the undocumented `/proxy/network/v2/api/site/{site}/static-dns/*` to the official `/proxy/network/integration/v1/sites/{siteId}/dns/policies/*` (requires Network 10.3.58+). |

## Community

Thanks to everyone in the [Home Operations](https://discord.gg/home-operations) Discord community.
