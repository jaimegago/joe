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
| `orders`   | 1        | mostly up, restarting | holds memory near its 128Mi limit; a short fast phase OOMKills a few times within ~5 min to seed real restarts, then bursts only every ~13 min → Running 1/1 near the limit with rare OOMKilled restarts, not CrashLoopBackOff |
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
- `orders` OOMKills + restarts: the container runs a two-phase workload keyed off
  a start counter it persists in an `emptyDir` — a **fast phase** for its first
  three starts that holds near the limit for ~15s then bursts over it, so real
  OOMKilled restarts accumulate within ~5 minutes of apply (first kill inside the
  first minute; `RESTARTS` reaches 2–3 by ~3–5 min), followed by a **steady
  phase** that holds near the limit and bursts only every ~13 minutes.

The ~13-minute steady interval is deliberate: the kubelet resets a container's
restart back-off timer only after it runs cleanly for 10 minutes (the back-off
caps at 300s and the reset threshold is 2× that = 600s), so spacing the steady
kills past that window keeps every steady restart at the base ~10s delay and the
pod out of `CrashLoopBackOff`. The fast phase intentionally accepts a few short
back-off waits up front (10s, 20s, 40s) to seed the restart history quickly.

Steady state for `orders` is **`Running` 1/1 near its memory limit**, with the
`RESTARTS` count climbing slowly (one at a time, ~13 min apart) and only a brief
few-second restart window at each OOM kill. It is *not* `CrashLoopBackOff` — if
that persists past the first few minutes, the pod is being killed faster than the
back-off resets.

`payments` and `checkout` settle within a couple of minutes. `orders` shows its
full symptom set (restarts + OOMKilled last state) within ~5 minutes; give it
~15 minutes total to confirm it then holds `Running` in the steady phase.

## Verify each symptom is live

**orders — OOM kills and restarts are real:**

```sh
# Pod is Running 1/1 with the RESTARTS column climbing slowly (~13 min apart);
# the last termination reason is OOMKilled with exit code 137:
kubectl get pods -n shop -l app.kubernetes.io/name=orders
kubectl describe pod -n shop -l app.kubernetes.io/name=orders | grep -A6 'Last State'
# The OOM shows up as a Terminated/OOMKilled last state (exit code 137) —
# kind/containerd records the kill on the pod, not as a namespace-scoped
# OOMKilling event:
kubectl get pod -n shop -l app.kubernetes.io/name=orders \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}' ; echo
kubectl get pod -n shop -l app.kubernetes.io/name=orders \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.exitCode}' ; echo
# A short-lived "Back-off restarting failed container" event appears for a few
# seconds right after each kill, then clears — it does not persist, because the
# back-off resets between the widely spaced kills (this is what keeps the pod
# out of CrashLoopBackOff). Run this just after a restart to catch it:
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
