# MaaS Router — Operations Runbook

Practical guide for deploying and operating MaaS Router in production.
Region: **ap-east-1** (Hong Kong). All commands assume `kubectl` is pointed at
the `maas-prod` EKS cluster and `aws` is authenticated to the prod account.

```sh
aws eks update-kubeconfig --name maas-prod --region ap-east-1
```

---

## Architecture recap

| Component     | Runs on                | Purpose                                              |
| ------------- | ---------------------- | --------------------------------------------------- |
| **web**       | EKS Deployment `web`   | Next.js — dashboard, admin, billing, playground, API |
| **proxy**     | EKS Deployment `proxy` | Go — LLM request router, rate limiting, usage logging |
| **RDS**       | Postgres (Multi-AZ)    | Users, API keys, usage logs, billing ledger         |
| **ElastiCache** | Redis                | Rate-limit counters, auth cache (fail-open)         |
| **EKS**       | `maas-prod` cluster    | Hosts web + proxy behind an ALB (LB Controller)     |

Ingress is an AWS ALB provisioned by the AWS Load Balancer Controller.
Secrets are synced from **AWS Secrets Manager** (`maas/prod`) into Kubernetes
Secrets by the **External Secrets Operator (ESO)**. Images live in **ECR**
(`maas-web`, `maas-proxy`).

---

## First-time deploy

Do these in order. Later stacks depend on outputs (VPC IDs, subnets, cluster
name) from earlier ones.

1. **Deploy CloudFormation stacks** (in order):
   1. `network` — VPC, subnets, NAT, security groups
   2. `data` — RDS Postgres + ElastiCache Redis
   3. `eks` — EKS cluster + node group + OIDC provider
   4. `app` — ECR repos, IAM roles (incl. the OIDC deploy role), ALB config
   5. `dns` — Route 53 records + ACM certificate
2. **Populate Secrets Manager** `maas/prod` with: `DATABASE_URL`, `REDIS_URL`,
   `NEXTAUTH_SECRET`, provider API keys, `STRIPE_SECRET_KEY`,
   `STRIPE_WEBHOOK_SECRET`, `RESEND_API_KEY`, `TURNSTILE_SECRET_KEY`.
3. **Install cluster add-ons**:
   - External Secrets Operator (ESO) — then apply the `ExternalSecret` /
     `SecretStore` manifests so `maas/prod` syncs into the `maas` namespace.
   - AWS Load Balancer Controller — required for the ALB ingress.
4. **Apply Kubernetes manifests**: `kubectl apply -f infra/k8s/`
   (namespace, deployments, services, ingress, ExternalSecrets).
5. **Build & deploy the app**:
   - Push to `main` → `build.yml` builds and pushes images to ECR.
   - Run `deploy-prod.yml` (workflow_dispatch) with the image tag (git SHA).
     This runs `prisma migrate deploy` then rolls out web + proxy.
6. **Promote the first admin** (no admin exists yet, so do it via SQL):
   ```sql
   UPDATE "User" SET is_admin = true WHERE email = 'you@example.com';
   ```

---

## Routine deploy

1. Merge the PR to `main`. CI (`ci.yml`) must be green.
2. `build.yml` runs automatically on push to `main` — builds `maas-web` and
   `maas-proxy`, tags each with the commit SHA and `latest`, pushes to ECR.
3. Trigger **deploy-prod.yml** (Actions → Deploy to Production → Run workflow)
   with `image_tag` = the git SHA from step 2.
4. Approve the `production` environment gate when prompted.
5. The workflow runs the migration, then `kubectl set image` + `rollout status`
   for both deployments. Watch the run to completion.

---

## Rollback

Roll back the deployments to the previous ReplicaSet:

```sh
kubectl rollout undo deployment/web   -n maas
kubectl rollout undo deployment/proxy -n maas
kubectl rollout status deployment/web   -n maas
kubectl rollout status deployment/proxy -n maas
```

**Database**: a code rollback does **not** undo an applied migration. If a
migration corrupted data, use **RDS point-in-time restore** to a timestamp just
before the deploy, then repoint `DATABASE_URL`. Prefer additive/backward-
compatible migrations so app rollback alone is sufficient.

---

## Alarms → actions

| Alarm                          | Likely cause                          | First action                                                              |
| ------------------------------ | ------------------------------------- | ------------------------------------------------------------------------ |
| Proxy **5xx > 5%**             | Bad deploy, provider outage           | Check proxy logs; roll back if deploy-correlated                          |
| Proxy **p99 > 30s**            | Slow provider, saturated pods         | Check provider status; scale proxy replicas                              |
| **Provider error > 20%**       | Provider outage or bad/expired key    | Verify key in Secrets Manager; check provider status page                 |
| **log_inbox_depth > 8000**     | Usage-log writer backed up            | Check DB write latency / RDS CPU; verify writer pod is healthy            |
| **Stripe webhook failures**    | Webhook secret mismatch, DB down      | Verify `STRIPE_WEBHOOK_SECRET`; check `/api/stripe-webhook` logs          |
| **RDS CPU > 80%**              | Query load / missing index / migration | Check slow queries; consider instance bump; pause heavy jobs             |
| **Revenue drop > 50%**         | Billing/checkout broken, provider down | Test checkout + a real completion; check Stripe + proxy dashboards        |

---

## Common incidents

### Provider key compromise
Rotate the key with the provider, update the value in **Secrets Manager**
(`maas/prod`). ESO resyncs into the cluster within its refresh interval; restart
the affected pods to pick it up immediately if needed. No code change required.

### Chargeback / dispute storm
Auto-suspend on dispute already handles the money side — disputed accounts are
suspended automatically. Operationally, watch **Admin → Users** for the review
tint on flagged accounts and confirm suspensions look correct. No manual
credit clawback needed.

### Redis down
Rate limiting and the auth cache **fail open** — requests still serve, just
without cache/limit enforcement. **Billing is unaffected** (it uses Postgres,
not Redis). Restore ElastiCache; no data loss since it is rebuildable.

### Migration failure
`deploy-prod.yml` runs the migration **before** `kubectl set image`. A failed
migration aborts the workflow, so the old pods keep serving the old schema —
**safe, no partial rollout**. Fix the migration and re-run the deploy.

---

## Backups

- **RDS**: automated daily snapshots, **7-day** retention. Point-in-time
  restore available within that window.
- **Before every migration**: take a **manual RDS snapshot** (retained beyond
  the 7-day window) so a bad migration can be reverted cleanly.
- **ElastiCache**: **not** backed up — it holds only rate-limit counters and
  auth cache, both rebuildable on restart.

---

## Cost monitoring

- **AWS Budget** alert at **$200/mo** — notifies on forecasted overspend.
- **Admin → Overview** shows **daily margin** (revenue vs. provider cost); watch
  it for margin compression or a sudden cost spike from a single account.
