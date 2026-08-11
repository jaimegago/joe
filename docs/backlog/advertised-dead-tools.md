Advertised-dead tools — tools registered on the loop that no registrable component can serve
Status: open
Priority: next

More than twenty tools registered in [internal/tools/default.go](../../internal/tools/default.go)
are advertised to the LLM on every task while no registrable component type can satisfy their
accessor guard assertion. They fall into five families: the datastore family (`postgres_stat`,
`postgres_query`, `mysql_stat`, `mysql_query`, `redis_info`, `redis_slowlog`, `mongodb_stat`,
`kafka_topics`, `kafka_brokers`, `kafka_consumer_groups`, `elasticsearch_health`,
`elasticsearch_indices`), the registry family (`registry_query`, `artifactory_query`,
`ecr_query`), the cloud family (`aws_ec2`, `aws_eks`, `aws_rds`, `aws_vpc`), the helm family
(`helm_releases`, `helm_get_release`, `helm_history`), and nginx (`nginx_ingresses`,
`nginx_status`, `nginx_config`).

Every call to one of these is a guaranteed failure, billed full price against the iteration
budget (D-0096), with error text that invites uninformed retries. The registry family is the
strongest form of the defect: per D-0058 those four types have no construction path at all, so
even a hand-inserted row can never yield a live adapter.

## Design options to settle before building

- Filter the task registry to tools whose types are registrable (or registrable-and-armed) at
  registry construction.
- Keep the tools advertised but return an honest structural error naming that no registrable
  component type serves the tool.
- Credential-wire the types per-family, following the repo-registration-path pattern (D-0150).

## Ledger of unpopulatable graph relations caused by the same unregistrable types

Fully unpopulatable: `ingress_for` (nginx-ingress), `stores_in` (five datastores), `queues_in`
(kafka), `image_stored_in` (registries, the dead-strongest form), `is_k8s_node` (aws and azure
both — notable because it is the only cloud-to-cluster bridging edge, so the graph cannot
connect a cluster to its hosting cloud account on any fresh install).

Partially affected, still populatable via a registrable producer: `metrics_in` and `logs_in`
(datadog unreachable), `managed_by` (helm unreachable).

Dead relations with zero production construction sites anywhere: `dashboard_in`,
`publishes_to`.
