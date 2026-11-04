# 🤖 Hosting elsewhere

A Rastrillo app is one static Go binary, a SQLite file and, if it
stores files, an S3-compatible bucket. Anything that keeps one process
running and gives it a persistent disk for the SQLite file can host it:
a server of your own, a container platform, a Kubernetes cluster. The
binary is the same one CARLOS runs, and it needs no code changes.

[Deploying](/docs/deploying) covers CARLOS. This page covers
everywhere else.

## What CARLOS did that you now do

On CARLOS the platform handles these. Elsewhere, each is yours to set
up or go without:

- **TLS.** A reverse proxy in front, or your host's own.
- **Backups.** Without continuous replication, the database lives on
  one disk.
- **The app's address and key**, set as environment variables.
- **Email**, through an SMTP relay.
- **File storage**, in a bucket you provide.
- **Schedules**, from cron or your host's scheduler.

Some CARLOS features do not come with the app: hibernation,
`carlos logs`, edge analytics, the edge's protection and rate limits,
canaries and channels. Your host may offer its own. Without
hibernation the process stays up, so [background jobs](/docs/jobs) run
until the next restart or deploy.
Try a change on a second copy of the app, with its own port and
database, before you replace the live binary.

## Where the app listens

`rastrillo.Resolve` and `rastrillo.Serve` pick the first of these
that applies:

1. A socket systemd hands over (`LISTEN_FDS`).
2. `-socket path`, a unix socket.
3. `-addr host:port`, or `Options.Addr` set in `main.go`.
4. `:$PORT`, when the `PORT` variable is set. Hosts such as Render and
   Railway set it.
5. `:8080`.

`PORT` must be a number from 1 to 65535. Anything else stops the app
with an error that names the value, rather than leaving it on 8080
where the host's health check would never find it. `PORT` is ignored
when an earlier rule applies, so `-addr` always wins. A `PORT` in your
own shell moves `go run .` too.

`:$PORT` and `:8080` listen on every interface, which is right inside a
container. On a server with a proxy in front, pass
`-addr 127.0.0.1:8080` so the app is not reachable around the proxy.

`-db path` names the database file. When systemd sets
`$STATE_DIRECTORY`, Rastrillo resolves a relative path inside it.

Point the host's health check at `GET /healthz`. On SIGTERM the app
stops accepting requests and waits up to 10 seconds for open ones to
finish.

## Environment variables

`<APP>` is the app's Go package name in capitals, as `main.go` reads
it: an app called `notes` reads `NOTES_ORIGIN`.

| Variable | What it is | When it is unset |
|---|---|---|
| `<APP>_ORIGIN` | The public `https://` address. The CSRF check and emailed sign-in links use it. | `http://localhost:8080`, with a warning. Emailed links point there, and the CSRF check compares posts with the wrong address. |
| `<APP>_INSTANCE_KEY` | A long random secret, for apps with sign-in. Keep it out of the repository. | A new key at every start, so pending sign-in links stop working after a restart. |
| `<APP>_SMTP_HOST`, `<APP>_SMTP_FROM` | Your SMTP relay and the sender address. Both are required to send. | Mail is written to the log, not sent. |
| `<APP>_SMTP_PORT`, `<APP>_SMTP_USER`, `<APP>_SMTP_PASS` | The relay's port, username and password. | |
| `CARLOS_STORE_BUCKET`, `_REGION`, `_ENDPOINT`, `_ACCESS_KEY`, `_SECRET_KEY` | Your S3-compatible bucket, for an app that stores files with [`blobs.S3FromEnv`](/docs/reference/blobs). | No bucket: `S3FromEnv` returns no store. |
| `CARLOS_ADMIN_TOKEN` | A long random secret that scheduled calls send as a bearer token. | `carlos.Tick` refuses every call. |

The `CARLOS_` names stay the same off CARLOS.

If you run a proxy in front of an app with sign-in, set
`TrustedProxyHops` in its `auth.Config` to the number of proxies,
usually 1. Without it, the per-address sign-in limit counts every
visitor as the proxy.

## Work on a clock

On CARLOS the platform calls the app's scheduled paths. Elsewhere, cron
or a systemd timer does, with the admin token as the bearer:

```sh
curl -fsS -X POST \
  -H "Authorization: Bearer $CARLOS_ADMIN_TOKEN" \
  https://myapp.example.com/jobs/sync
```

The handler is the same one: guarded by `carlos.Tick` and exempt from
the CSRF check with `csrf.Exempt`, as
[Deploying](/docs/deploying#work-on-a-clock) shows. Give the cron job
the same token the app has.

`carlos.ScheduleAt` returns `carlos.ErrNotOnCarlos` off CARLOS. For
one-off work, store what is due as rows, and have a regular tick handle
the ones whose time has come.

## A plain server

Build for the server's processor (most are x86) with the scaffold's
Makefile, which stamps the version from git, so commit first:

```sh
make release RELEASE_GOARCH=amd64
```

Copy `releases/myapp-linux-amd64` to the server as
`/usr/local/bin/myapp`, then run it as a systemd service:

```text
# /etc/systemd/system/myapp.service
[Unit]
Description=myapp
After=network-online.target

[Service]
User=myapp
ExecStart=/usr/local/bin/myapp -addr 127.0.0.1:8080 -db myapp.db
EnvironmentFile=/etc/myapp/env
StateDirectory=myapp
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`StateDirectory` makes `/var/lib/myapp`, and the relative `-db` puts
the database there. `/etc/myapp/env` holds the variables above, one
`NAME=value` a line, readable only by root.

For socket activation, let systemd hold the port instead. Drop `-addr`
from `ExecStart` and add a socket unit. A restart then queues new
connections rather than refusing them:

```text
# /etc/systemd/system/myapp.socket
[Socket]
ListenStream=127.0.0.1:8080

[Install]
WantedBy=sockets.target
```

Put a proxy in front for TLS. Caddy fetches and renews the certificate
itself:

```text
myapp.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

For backups, run [Litestream](https://litestream.io) as its own service,
replicating `/var/lib/myapp/myapp.db` to a bucket. Rastrillo opens
SQLite in WAL mode, which Litestream needs. Its documentation covers
the configuration and restoring.

## A container image

A static Go binary runs on the distroless static image, which includes
CA certificates:

```text
FROM gcr.io/distroless/static
COPY releases/myapp-linux-amd64 /myapp
ENTRYPOINT ["/myapp", "-db", "/data/myapp.db"]
```

Build the binary with `make release RELEASE_GOARCH=amd64` (or `arm64`
for an ARM host) before `docker build`. The image passes no `-addr`, so
the app listens on `$PORT` where the host sets it and on 8080 where it
does not.

Mount a persistent volume at `/data`. Without one, the database is lost
on every deploy.

## Container platforms

Fly, Render, Railway, Miren and similar hosts run the image as one
service. Their limits and plans change, so check the provider's current
documentation. What holds across them:

- **One instance per volume.** SQLite lives on the volume, so the app
  cannot scale out, and a deploy has a short gap while the volume moves
  to the new machine.
- **The port.** On a host that sets `PORT`, there is nothing to do. On
  one that does not, tell it the app listens on 8080.
- **Secrets.** Set the environment variables above as the provider's
  secrets.
- **Schedules.** Point the provider's cron or scheduled jobs at the
  scheduled path with the admin-token bearer.
- **Backups.** Some providers snapshot volumes; check how often.
  Litestream to a bucket adds point-in-time recovery.
- **Scale to zero, where offered**, behaves like hibernation:
  background jobs end when the machine stops, so keep them idempotent.
- **Miren.** Run the web service as one fixed instance with a disk at
  `/data`. Its scheduled tasks cannot reach that disk, so a task calls
  the app's scheduled path over HTTP.

## Kubernetes

Run exactly one replica, so two pods never open the same database: a
StatefulSet, or a Deployment with the `Recreate` strategy.

- The image above, listening on 8080, with a `ReadWriteOnce`
  PersistentVolumeClaim at `/data`.
- A Service and an Ingress, with the cluster's TLS.
- Secrets for the environment variables above, including a long random
  `CARLOS_ADMIN_TOKEN`.
- A CronJob per schedule that calls the scheduled path with that token
  as the bearer.
- Litestream as a sidecar container replicating `/data` to a bucket,
  since a volume snapshot is not point-in-time recovery.

A deploy restarts the one pod, so there is a short gap.

## Moving an app off CARLOS

Bring the database file and point the app at the same bucket. If the
app seals data with its instance key, keep the key: set
`<APP>_INSTANCE_KEY` to the one it had on CARLOS. Before you move
everything, run one copy on the new host to prove the binary and data
work there.
