# Ansible collection `alex0ptr.aigude`

Installs [AI Gude](https://github.com/alex0ptr/aigude) on a Linux host with rootful Podman, as Quadlets, which are
systemd units. It can also install Traefik in front of it, with HTTPS and a sign-in, and a Firecrawl of your own. You set
Ansible variables and the roles do the rest. Secrets, the inventory and the firewall of your host stay in your
repository.

This is an example of how to deploy AI Gude, and it is vibe coded: an AI assistant wrote it. The roles were run on a
host and worked. Nobody has reviewed the code, so be especially careful. The only checks the repository runs on it are `ansible-lint` and
`ansible-playbook --syntax-check`.

## Roles

| Role | Does | Optional |
|---|---|---|
| `podman` | Installs Podman, Netavark and Aardvark DNS, checks that Podman is 4.8 or newer, can create a systemd target | yes |
| `traefik` | Traefik with certificates from an ACME server. Every service puts a route file in its `dynamic` folder | yes |
| `firecrawl` | A Firecrawl of your own: five containers on a network of their own | yes |
| `aigude` | AI Gude: system user, folders, environment file, image, Quadlet. With `aigude_proxy: traefik` also the route and the sign-ins | no |

The roles are meant for Ubuntu 24.04 and Debian 13. Podman older than 4.8, as on Ubuntu 22.04, is not supported because
the roles use `.image` units. The `podman` role stops with a message on such a host.

## Install

```yaml
# requirements.yml
collections:
  - name: https://github.com/alex0ptr/aigude.git#/deploy/ansible_collections/alex0ptr/aigude
    type: git
    version: main
```

```bash
ansible-galaxy collection install -r requirements.yml
```

`version` is a branch or a tag. Use a tag `v…` once there is one. The collection needs Ansible 2.19 or newer and
`community.general`, which is installed with it.

## The four setups

Each setup has an example playbook in [playbooks/](playbooks/). Copy it, name your host group and set the variables.

| Proxy | Firecrawl | Playbook | Roles |
|---|---|---|---|
| none | hosted | [no-proxy-hosted-firecrawl.yml](playbooks/no-proxy-hosted-firecrawl.yml) | `podman`, `aigude` |
| none | your own | [no-proxy-own-firecrawl.yml](playbooks/no-proxy-own-firecrawl.yml) | `podman`, `firecrawl`, `aigude` |
| Traefik | hosted | [traefik-hosted-firecrawl.yml](playbooks/traefik-hosted-firecrawl.yml) | `podman`, `traefik`, `aigude` |
| Traefik | your own | [traefik-own-firecrawl.yml](playbooks/traefik-own-firecrawl.yml) | `podman`, `traefik`, `firecrawl`, `aigude` |

The smallest setup needs `aigude_gemini_api_key`. For the hosted Firecrawl it also needs `aigude_firecrawl_url` set to
`https://api.firecrawl.dev` and `aigude_firecrawl_api_key`. AI Gude then listens on `127.0.0.1:8080` of the host.

Every variable of a role has a type, a default and a description in its `meta/argument_specs.yml`. Ansible checks your
values against it before the first task. `ansible-doc -t role alex0ptr.aigude.aigude` shows the same.

## Things to know

**There is no sign-in in AI Gude.** Without a proxy the port is published on `127.0.0.1` only. With `aigude_proxy:
traefik` the role writes a route that asks for HTTP Basic Auth and tells AI Gude who signed in. Any other proxy must do
what the section
[Configure Multiple Users and Authentication](https://github.com/alex0ptr/aigude#configure-multiple-users-and-authentication)
of the README describes. It sets `X-Aigude-User` to the Id of the User, removes `X-Aigude-Demo` and is the only way to
the port.

**The sign-in name is the Id of a User.** Give each entry of `aigude_users` a User Config of the same name in
`aigude_user_configs`, or the User is answered with 403. The role warns about a sign-in without a file. User Configs are
written once and never replaced, because the interface edits them. Give passwords as `password_hash`, a bcrypt hash. A
clear text `password` is hashed on the host with `python3-passlib` and `python3-bcrypt`. The role installs them unless
`aigude_install_htpasswd_dependencies` is false, and nothing in clear text reaches the disk. Traefik reads the file of
the sign-ins only when it loads the route and does not watch it. A new or changed sign-in therefore restarts
`aigude_traefik_service`, which is `traefik.service`. Set it empty if another tool runs your Traefik.

**The image.** The default image is `ghcr.io/alex0ptr/aigude:<aigude_version>`. It exists once a tag `v…` of the
repository is published. Until then set `aigude_image` to another image, or build an archive and let the role load it:

```bash
podman save --output aigude.tar localhost/aigude:local
```

```yaml
aigude_image: localhost/aigude:local
aigude_image_archive: aigude.tar
```

With an archive nothing is pulled and the container names `aigude_image` directly. Without one every image has an
`.image` unit that pulls it in a service of its own, with a timeout of 15 minutes because the images of the Firecrawl are
large. That unit pulls the registry whenever it starts, also for an image pinned by digest. A host without network at boot
does not start the container until the pull works, and the unit tries again every 30 seconds.

**Two networks, on purpose.** Traefik and AI Gude share the network `web`. AI Gude and the Firecrawl share `firecrawl`.
The API of the Firecrawl has no authentication and fetches any address it is given, so Traefik must not be able to reach
it. It publishes no port unless you set `firecrawl_publish`. A role owns the network of its service, and `aigude` only
names it with `aigude_networks`.

**The firewall is yours.** The roles change ufw only when you set `traefik_manage_ufw` or `firecrawl_manage_ufw`. They
then write rules only for the network of their own service, which needs a fixed subnet in `traefik_network_subnet` or
`firecrawl_subnet`. Nothing is removed again when you turn the switch off. A published port reaches its container by
forwarding and not by the INPUT chain. ufw denies routed packets and Netavark lets only `ESTABLISHED` and `RELATED` ones
through, so `ufw allow 80` does not help. The rule is `ufw route allow proto tcp to <subnet> port 80`. Without it the
service answers on the host and times out from outside. Container DNS needs `ufw allow from <subnet> to any port 53`.
With another firewall, add the equivalent rules yourself.

**Start with the staging server.** Set `traefik_acme_staging: true`, check that the route works, then set it to `false`
and run again. Staging and production keep their certificates in separate files.

**Back up** `aigude_data_dir`, which holds the database and the audio files, and the User Configs.

## What the Quadlets avoid

The Quadlet generator accepts all of the following mistakes, so the units avoid them and you should not undo it:

- Playwright runs without `NoNewPrivileges=true`. With it the process cannot open UDP sockets, so every DNS lookup
  fails and Playwright never starts.
- RabbitMQ runs as `User=rabbitmq:rabbitmq`. Its health check runs as root and would create a root-owned
  `.erlang.cookie` before the server writes its own.
- Every container unit has `RestartSec=5`, and the roles run `systemctl reset-failed` before they start a unit, because
  of the start limit of systemd.
- The configuration folder is mounted without `:U`. The role sets owner and mode itself.
- The Firecrawl API requires Postgres, RabbitMQ and Redis. It only wants Playwright, so that a failing Playwright does
  not take the API down. Scraping pages that need a browser fails until Playwright runs again.
