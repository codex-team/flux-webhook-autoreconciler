# flux-webhook-autoreconciler

This project aims to solve the problem of having to manually setup webhooks for each repository in a cluster to reconcile Flux sources. 

Normally, you'd need to set up a Receiver for each source, then a webhook for that receiver ([official docs](https://fluxcd.io/flux/guides/webhook-receivers/)). 
This can get pretty annoying and it's easy to mess up, especially when you have tons of repos you want to deploy with Flux.

This project tackles this by giving you a single webhook receiver. You hook it up to your entire GitHub organization, 
and it'll automatically keep the Flux sources in sync across all your repos.

## How it works

This project has two main parts:

- `Server`: It gets the webhooks, reconciles the sources, and tells the clients about what happened.
- `Client`: (Optional) It listens to the server and reconciles the sources. You can run just the server if you want, but having a client is handy if you have multiple clusters. You send one webhook to the server, and it’ll reconcile the sources in all your clusters through their clients.

Basically, the server waits for webhooks on the `/webhook` endpoint, and the client connects to the server on the `/subscribe` endpoint using WebSockets. You can have as many clients as you want (like, one client for each Kubernetes cluster). Both the server and client take care of reconciling the sources. 

To figure out which sources need reconciling when a webhook comes in, the reconciler takes the package name from the webhook data, checks out all the sources, and then matches it up with the package name in each source. If there's a match, that source gets reconciled.

## Installation

There is helm chart available published as OCI artifact in GitHub Packages [here](https://github.com/codex-team/flux-webhook-autoreconciler/pkgs/container/flux-webhook-autoreconciler%2Fchart%2Fflux-webhook-autoreconciler).
You can install it using [Helm CLI](https://helm.sh/docs/topics/registries/) or by using Flux itself:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: flux-webhook-autoreconciler
spec:
  interval: 24h
  type: oci
  url: oci://ghcr.io/codex-team/flux-webhook-autoreconciler/chart
---
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: flux-webhook-autoreconciler
spec:
  interval: 24h
  timeout: 5m
  chart:
    spec:
      chart: flux-webhook-autoreconciler
      version: '0.0.8' # replace with the latest version from here https://github.com/codex-team/flux-webhook-autoreconciler/pkgs/container/flux-webhook-autoreconciler%2Fchart%2Fflux-webhook-autoreconciler
      sourceRef:
        kind: HelmRepository
        name: flux-webhook-autoreconciler
      interval: 24h
  values:
    config:
      values:
        mode: server
    secrets:
      existingSecret: flux-webhook-autoreconciler
      githubSecretKey: github_secret
      subscribeSecretKey: subscribe_secret
    networkPolicy: # it can be necessary if you install this into flux-system namespace, because it will block the traffic
      enabled: true
```

## Configuration

The configuration is done via YAML file that is passed to the container via `--config` flag (by default it's `config.yaml` in your current directory).

You can find the example configuration in [config](./config) folder both for `server` and `client` modes.

You'll also need to set up a GitHub webhook. You have the choice to do this for your whole organization or on a per-repo basis. For the how-to, check out the [official docs](https://docs.github.com/en/webhooks/using-webhooks/creating-webhooks). 
To get the webhook working, you'll need to sort out a few things:

- Payload URL: `https://<your-domain>/webhook`
- Content type: `application/json`
- Secret (optional but recommended)
- In the section "Which events would you like to trigger this webhook?", go for "Let me select individual events." and then tick the box for the "Registry packages" event.

And that’s it! Now you can push a new package to your GitHub registry and it will be automatically reconciled by Flux.

## Todo

- [ ] Add support for other kinds of sources. Right now, it’s just `OCIRepository`.
- [ ] Make it work with other types of webhook data. For now, it’s only set up for GitHub-like payloads.
- [ ] Add different filtering abilities, like filtering by package name or repo labels.

# Contribute

Feel free to contribute to this project by creating issues and pull requests.