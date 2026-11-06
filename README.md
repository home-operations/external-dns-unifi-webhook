# ExternalDNS Webhook Provider for UniFi

[![CI](https://github.com/home-operations/external-dns-unifi-webhook/actions/workflows/ci.yaml/badge.svg)](https://github.com/home-operations/external-dns-unifi-webhook/actions/workflows/ci.yaml)
[![Release](https://img.shields.io/github/v/release/home-operations/external-dns-unifi-webhook)](https://github.com/home-operations/external-dns-unifi-webhook/releases)
[![License](https://img.shields.io/github/license/home-operations/external-dns-unifi-webhook)](LICENSE)
[![Discord](https://img.shields.io/discord/673534664354430999?label=discord&logo=discord&logoColor=white&color=blue)](https://discord.gg/home-operations)

A webhook provider for [ExternalDNS](https://github.com/kubernetes-sigs/external-dns) that manages DNS records in a UniFi Network controller. ExternalDNS keeps DNS in sync with your Kubernetes Ingresses and Services. This provider writes those records to the built-in DNS of UniFi through the `static-dns` API of the Network application.

## Requirements

| Component     | Minimum version |
| ------------- | --------------- |
| ExternalDNS   | v0.21.0         |
| UniFi OS      | 5.x             |
| UniFi Network | 10.3.58         |

## How it works

The provider runs as a sidecar next to the ExternalDNS controller. It speaks the ExternalDNS webhook protocol on one side and the UniFi Network API on the other. Records go through `/proxy/network/v2/api/site/{site}/static-dns`. At startup, the provider resolves the site name once through the `/proxy/network/integration/v1/sites` endpoint of the [Integration API](https://developer.ui.com/network/). Ubiquiti does not document the `static-dns` endpoints. Unlike the DNS Policies endpoints of the Integration API, they accept names whose labels start with an underscore, for example the RFC 9462 DDR name `_dns.resolver.arpa`.

The provider reaches UniFi in one of two ways:

- Local (default): the provider connects directly to the controller on your network.
- Cloud connector: the provider sends requests through `api.ui.com` to a console that you cannot reach on the LAN. See [Cloud connector](#cloud-connector).

The ExternalDNS controller applies domain filters, not this webhook. See [Domain filtering](#domain-filtering).

## Limitations

UniFi uses [dnsmasq](https://dnsmasq.org) as its DNS backend, so the provider has the same limits:

- UniFi does not support wildcard names such as `*.example.com`.
- UniFi permits one CNAME per name. If the webhook creates a CNAME where one already exists, it deletes the existing record first. If ExternalDNS sends more than one target for one CNAME, the webhook uses only the first target. It drops the other targets and logs a warning.

## Quick start

### 1. Create a UniFi API key

Every request authenticates with an API key. The provider does not support username and password authentication.

For a local controller, log into your console by IP. Go to Settings → Control Plane → Integrations → Create API Key. Copy the key.

> Only a Super Admin can create an API key. The key continues to work after you downgrade the user. For least privilege, create a dedicated `external-dns` user. While this user is a Super Admin, generate its key. Then change the role of the user to Site Admin. A Site Admin can manage DNS records.

For the cloud connector, create an account-level key in the [UniFi Site Manager](https://unifi.ui.com) under account settings → API. This key is not the same as a local key for one console.

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
| `UNIFI_SITE`                | Site name (for example `default`) or site UUID. Resolved to the site name at startup. | `default` |
| `UNIFI_CONSOLE_ID`          | Console ID. Setting this routes requests through the `api.ui.com` cloud connector.    | —         |
| `UNIFI_SKIP_TLS_VERIFY`     | Skip TLS verification. Ignored in cloud mode.                                         | `true`    |
| `UNIFI_CA_CERT`             | Path to a PEM bundle of extra trusted CAs (alternative to skipping verification).     | —         |
| `UNIFI_APPLY_WORKERS`       | Maximum concurrent record operations during a reconcile.                              | `5`       |
| `UNIFI_RETRY_ATTEMPTS`      | Total attempts per request (including the first).                                     | `3`       |
| `UNIFI_RETRY_INITIAL_DELAY` | Initial backoff before the first retry.                                               | `500ms`   |
| `UNIFI_RETRY_MAX_DELAY`     | Maximum backoff between retries (also caps `Retry-After`).                            | `10s`     |
| `UNIFI_REQUEST_TIMEOUT`     | Per-attempt timeout for controller requests. Unset disables it (see below).           | —         |

#### Request timeouts

ExternalDNS gives each call to the webhook a fixed time budget: `--webhook-provider-read-timeout` plus `--webhook-provider-write-timeout` (5s + 10s by default). If the webhook does not answer within this budget, ExternalDNS treats the call as a fatal error and exits. If the webhook returns an HTTP 5xx, ExternalDNS only logs a soft error. Without a timeout per attempt, a controller that accepts the connection and then stalls keeps the webhook waiting past that budget. ExternalDNS then restarts instead of a retry on the next sync.

Set `UNIFI_REQUEST_TIMEOUT` to limit each attempt. If an attempt times out before the controller answers, the webhook retries `GET` and `DELETE` requests as it does for other network errors. A stall after the response headers fails without a retry. In both cases, the webhook answers `500` after it gives up. Keep `UNIFI_RETRY_ATTEMPTS × UNIFI_REQUEST_TIMEOUT` plus the backoff between attempts below the ExternalDNS budget. With the defaults (3 attempts, 500ms initial delay, up to 50% jitter), a `4s` timeout gives a worst case of about 14.3s, which is below the 15s default. As an alternative, raise the ExternalDNS timeouts. Keep their sum below `SERVER_WRITE_TIMEOUT`.

#### Cloud connector

To manage a remote console without a controller exposed on your LAN, set `UNIFI_CONSOLE_ID`. Set `UNIFI_HOST` to `https://api.ui.com`. The webhook then sends requests through `/v1/connector/consoles/{consoleId}/proxy/network/...`. The API is the same as for a local connection. Only the routing prefix changes.

- Use an account-level API key from the [UniFi Site Manager](https://unifi.ui.com), not a local key for one console.
- Find your console ID with the Site Manager API (`GET https://api.ui.com/v1/hosts`) or in the console URL at [unifi.ui.com](https://unifi.ui.com).
- The webhook ignores `UNIFI_SKIP_TLS_VERIFY`. `api.ui.com` presents a publicly-trusted certificate. If the webhook skipped verification, it exposed your API key.
- Reconciliation depends on the availability and the rate limits of the Ubiquiti API. The built-in backoff handles `429` responses, but a local connection is better when you can reach the controller.
- The connector forwards the `static-dns` paths today. Ubiquiti stated that the connector is meant to reach only the public integration API of each application, and on 2026-11-05 it refused those paths for a few hours. If that refusal becomes permanent, cloud mode stops until this webhook adapts.

#### Domain filtering

Configure `--domain-filter` (and its variants) on the ExternalDNS controller, not on this webhook. UniFi has no zones that the webhook can filter by, so the webhook follows the [ExternalDNS `GetDomainFilter` contract](https://github.com/kubernetes-sigs/external-dns/blob/v0.22.0/docs/contributing/sources-and-providers.md#implementing-getdomainfilter) and leaves the filter to the controller.

### Webhook server

| Variable                     | Description                                                | Default           |
| ---------------------------- | ---------------------------------------------------------- | ----------------- |
| `SERVER_HOST`                | Address that the webhook server binds to.                  | `localhost`       |
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

The webhook serves `/healthz` and `/readyz` on both ports. Kubernetes probes can therefore target the webhook port directly. You do not have to expose a second container port through the chart.

## Upgrading

The move to UniFi Network 10.3.58 introduced breaking changes:

| Setting                                                                                            | Change                                                                                                                          |
| -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `UNIFI_EXTERNAL_CONTROLLER`                                                                        | **Removed.** Point `UNIFI_HOST` at the controller directly, or use the [cloud connector](#cloud-connector) for remote consoles. |
| `DOMAIN_FILTER`, `EXCLUDE_DOMAIN_FILTER`, `REGEXP_DOMAIN_FILTER`, `REGEXP_DOMAIN_FILTER_EXCLUSION` | **Removed.** Configure `--domain-filter` on the ExternalDNS controller instead (see [Domain filtering](#domain-filtering)).     |
| `LOG_FORMAT=test`                                                                                  | **Renamed** to `LOG_FORMAT=text`.                                                                                               |
| Site lookup                                                                                        | `UNIFI_SITE` is resolved through the official `/proxy/network/integration/v1/sites` endpoint (requires Network 10.3.58+).       |

The move of records to the `static-dns` endpoints changed the `operation` label value on `externaldns_webhook_unifi_retries_total` and `externaldns_webhook_unifi_rate_limits_total` from `dns_policies` to `static_dns`. Update any alert or dashboard selectors that match the old value.

## Community

Thanks to everyone in the [Home Operations](https://discord.gg/home-operations) Discord community.
