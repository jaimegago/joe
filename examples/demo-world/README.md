# demo-world

A tiny, fictional three-service "shop" whose failure symptoms are **real cluster
state**, not scripted output — for recording Joe demo clips and for anyone who
wants a reproducible world to point an infra copilot at.

The world is openly fictional (there is no real orders/payments/checkout
business here), but every symptom below is a genuine Kubernetes condition: a real
kernel OOM kill from a memory limit, a real readiness probe returning a real 404,
real Service DNS. Nothing is faked or hand-authored into an event stream.

## The world

Namespace `shop`, three Deployments, one Service each:

| Service    | Replicas | Health   | Engineered symptom                                                        |
|------------|----------|----------|---------------------------------------------------------------------------|
| `orders`   | 1        | crashing | holds memory near its 128Mi limit and periodically bursts past it → OOMKilled + restarts |
| `payments` | 2        | degraded | readiness probe hits a missing `/healthz` → real 404 → pods NotReady, Deployment stays up |
| `checkout` | 2        | healthy  | fully ready; carries `PAYMENTS_URL` / `ORDERS_URL` env vars naming the other two services |

`checkout`'s two dependency URLs point at the in-cluster Service DNS names of
`payments` and `orders`, so the dependency edges are discoverable just by reading
its pod spec.

## Prerequisites

- Any local Kubernetes cluster and a `kubectl` pointed at it. A
  [kind](https://kind.sigs.k8s.io/) cluster is the easiest:

  ```sh
  kind create cluster --name joe-demo-world
  ```

- All images are public, unauthenticated Docker Hub images
  (`python:3.12-slim`, `nginx:1.27-alpine`) — no custom builds, no private
  registry.

## Stage

From the repo root:

```sh
kubectl apply -f examples/demo-world/
```

The `00-` prefix on the namespace file makes it apply before the workloads that
land in it.

## Reset

Delete the namespace (which removes everything in it) and re-apply:

```sh
kubectl delete namespace shop
kubectl apply -f examples/demo-world/
```

Re-staging reproduces all three symptoms from a clean slate.

## Expected time-to-symptom

- `payments` NotReady: within ~30 seconds of staging (first readiness probe).
- `checkout` Ready: within ~30 seconds of staging.
- `orders` first OOMKill + restart: within ~2 minutes; restarts keep
  accumulating on roughly a one-minute cadence thereafter, with the pod running
  near its limit in between.

Give the world a couple of minutes to settle before recording.

## Verify each symptom is live

**orders — OOM kills and restarts are real:**

```sh
# Restart count climbs (RESTARTS column) and the last termination reason is OOMKilled:
kubectl get pods -n shop -l app.kubernetes.io/name=orders
kubectl describe pod -n shop -l app.kubernetes.io/name=orders | grep -A6 'Last State'
# The OOM shows up as a Terminated/OOMKilled last state (exit code 137) plus
# Back-off restarting events — kind/containerd records the kill on the pod, not
# as a namespace-scoped OOMKilling event:
kubectl get pod -n shop -l app.kubernetes.io/name=orders \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}' ; echo
kubectl get events -n shop | grep -i back-off
```

**payments — pods are NotReady from a real probe failure, Deployment still up:**

```sh
# READY column shows 0/1 on the pods; the Deployment object is present:
kubectl get pods -n shop -l app.kubernetes.io/name=payments
kubectl get deployment payments -n shop
# Real probe-failure events referencing a 404 from the container:
kubectl get events -n shop --field-selector reason=Unhealthy
```

**checkout — healthy, dependency env vars discoverable in the spec:**

```sh
kubectl get pods -n shop -l app.kubernetes.io/name=checkout
kubectl get deployment checkout -n shop \
  -o jsonpath='{.spec.template.spec.containers[0].env}' ; echo
```

## Teardown

```sh
kubectl delete namespace shop
# or drop the whole throwaway cluster:
kind delete cluster --name joe-demo-world
```
