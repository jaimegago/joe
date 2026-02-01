# Joe Helper Files Generation Prompt

This prompt is designed for AI coding assistants (Claude Code, Cursor, GitHub Copilot, etc.) to generate `.joe/` helper files that accelerate Joe's infrastructure discovery.

---

## What Are .joe/ Files?

`.joe/` files are an **LLM-to-LLM cache**. You (a coding LLM) create them, and Joe's LLM consumes them.

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  You (Coding LLM)              .joe/ files              Joe's LLM   │
│                                                                      │
│  Analyze source code ────► Write understanding ────► Read & act     │
│  (expensive, slow)           (cached)               (cheap, fast)   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Key insight:** Joe's LLM will interpret these files—they don't need to be perfectly structured. Write them to be clear to another LLM, not to pass a strict YAML parser.

**Why this matters:**
- Without `.joe/`: Joe's LLM reads 50+ source files (~10k tokens, 30-60 seconds)
- With `.joe/`: Joe's LLM reads 3-5 small files (~1k tokens, 2-3 seconds)
- Cached: Joe replays previous interpretation (~0 tokens, instant)

---

## Instructions for AI Coding Assistant

You are analyzing a code repository to generate `.joe/` helper files for Joe, an AI-powered infrastructure operator tool.

**Your goal:** Create files that describe the **structure and relationships** in this repository, NOT the actual configuration values.

**Critical rule:** `.joe/` files contain **concepts and pointers**, never values. Tell Joe "look here for X" rather than copying configuration.

**Flexibility:** Since Joe's LLM interprets these files, you have flexibility in how you express things. Be clear and consistent, but don't worry about strict schema compliance. If something doesn't fit the examples, describe it in a way another LLM would understand.

### Examples of What TO Include

```yaml
# GOOD - pointer to where database config lives
- type: postgresql
  reference: "primary database"
  config_location: helm/values.yaml
  config_path: "database.host"
```

### Examples of What NOT TO Include

```yaml
# BAD - actual value copied from config
- type: postgresql
  url: postgres.prod.company.com    # ❌ Don't include actual URLs
  password: secret123               # ❌ Never include secrets
```

---

## Files to Generate

Analyze the repository and create the following files in `.joe/` directory:

### 1. `.joe/manifest.yaml` (Required)

Describes what this repository is.

```yaml
joe_version: "1.0"

repo:
  type: <helm_chart|kustomize|terraform|application|library|monorepo>
  name: <service or project name>
  description: <one-line description of purpose>
  team: <owning team if identifiable>
  language: <primary language if application code>
  
config:
  # List configuration files and what they contain
  helm_values:
    - path: <relative path>
      description: <what this file configures>
  # Or kustomize, terraform, docker-compose, etc.
  
environments:
  # If multiple environments are defined
  - name: <env name>
    values_file: <path to env-specific config>

# For monorepos
services:
  - name: <service name>
    path: <path within repo>
    description: <what it does>
```

### 2. `.joe/sources.yaml` (Required)

Infrastructure sources this code references or depends on.

```yaml
sources:
  - type: <postgresql|mysql|redis|kafka|rabbitmq|elasticsearch|etc>
    reference: <human description, e.g., "user data store">
    config_location: <file where connection is configured>
    config_path: <JSONPath or key path to the value>
    environments: [<list of envs where this applies>]
    used_by: [<list of services/components that use this>]
    purpose: <optional: what it's used for>
    
  - type: external_api
    name: <API name, e.g., Stripe>
    reference: <what it's used for>
    config_location: <file>
    config_path: <path>
    client_location: <optional: where API client code lives>
    
  - type: kafka
    reference: <description>
    config_location: <file>
    config_path: <path to brokers config>
    topics:
      - name_pattern: <pattern like "payment-*" or exact name>
        defined_in: <file where topic names are defined>
        purpose: <what these topics are for>
```

**Source types to look for:**
- Databases: `postgresql`, `mysql`, `mongodb`, `redis`, `elasticsearch`
- Messaging: `kafka`, `rabbitmq`, `sqs`, `pubsub`
- External APIs: `external_api` (Stripe, Twilio, SendGrid, etc.)
- Storage: `s3`, `gcs`, `minio`
- Other services: `http_service`, `grpc_service`

### 3. `.joe/topology.yaml` (Required for applications)

How this service connects to other services.

```yaml
this_service: <service name>

calls:
  # Synchronous calls to other services
  - service: <target service name>
    protocol: <http|grpc|graphql>
    purpose: <why it calls this service>
    endpoint_defined_in: <file where client/endpoint is configured>
    
publishes_to:
  # Async events this service produces
  - type: <kafka|rabbitmq|sqs|sns>
    topic_pattern: <topic name or pattern>
    event_types: [<list of event types if known>]
    schema_location: <optional: where event schemas are defined>
    producer_location: <file where producer code lives>
    
subscribes_to:
  # Async events this service consumes
  - type: <kafka|rabbitmq|sqs>
    topic_pattern: <topic name or pattern>
    handler_location: <file where consumer/handler code lives>
    
external_calls:
  # Calls to external APIs
  - name: <API name>
    client_location: <file or directory with client code>
    purpose: <optional: what it's used for>

databases:
  # Direct database access
  - type: <postgresql|mysql|etc>
    reference: <which database from sources.yaml>
    access_pattern: <read|write|read-write>
    orm_location: <optional: where models/entities are defined>
```

### 4. `.joe/flows.yaml` (Optional, if business flows are identifiable)

Business flows this service participates in.

```yaml
participates_in:
  - flow: <flow name, e.g., checkout, user-registration>
    role: <what this service does in the flow>
    position: <optional: numeric position in flow>
    receives_from: <service or topic that triggers this>
    passes_to: <service or topic this hands off to>
    
owns:
  # Flows that originate from this service
  - flow: <flow name>
    description: <what this flow does>
    steps:
      - <step 1 description>
      - <step 2 description>
```

### 5. `.joe/glossary.yaml` (Optional, for domain-specific terminology)

Terms that help Joe understand this codebase.

```yaml
terms:
  <term>:
    meaning: <definition>
    relevance: <why it matters for this repo>
    implementation: <optional: where it's implemented>
    see_also: <optional: link or reference>
```

---

## Analysis Process

1. **Identify repo type:**
   - Look for `Chart.yaml` (Helm), `kustomization.yaml` (Kustomize), `*.tf` (Terraform)
   - Look for `go.mod`, `package.json`, `requirements.txt`, `Cargo.toml` (application)
   - Look for `docker-compose.yaml`, `Dockerfile`

2. **Find configuration files:**
   - `values.yaml`, `values-*.yaml` (Helm)
   - `config.yaml`, `application.yaml`, `settings.py`, `.env.example`
   - `terraform.tfvars`, `*.tf`

3. **Extract infrastructure references (but not values):**
   - Database connection strings → note the config path
   - Kafka/messaging broker configs → note where defined
   - External API URLs → note the service name and config location
   - Service-to-service calls → note the client location

4. **Map service topology:**
   - Look for HTTP/gRPC client code
   - Look for Kafka producers/consumers
   - Look for database access patterns

5. **Identify business flows (if possible):**
   - Look for handler chains
   - Look for saga/workflow patterns
   - Look for event sequences

---

## Example Output

For a payment service with Helm charts:

**.joe/manifest.yaml**
```yaml
joe_version: "1.0"

repo:
  type: helm_chart
  name: payment-service
  description: "Processes payments via Stripe, handles refunds"
  team: payments
  language: go

config:
  helm_values:
    - path: helm/values.yaml
      description: "Base configuration"
    - path: helm/values-prod.yaml
      description: "Production overrides"
    - path: helm/values-staging.yaml
      description: "Staging overrides"

environments:
  - name: prod
    values_file: helm/values-prod.yaml
  - name: staging
    values_file: helm/values-staging.yaml
```

**.joe/sources.yaml**
```yaml
sources:
  - type: postgresql
    reference: "payment transactions database"
    config_location: helm/values.yaml
    config_path: "database.host"
    environments: [prod, staging]
    used_by: [payment-service, payment-worker]
    
  - type: kafka
    reference: "event streaming"
    config_location: helm/values.yaml
    config_path: "kafka.brokers"
    topics:
      - name_pattern: "payment-events-*"
        defined_in: internal/events/topics.go
        purpose: "payment lifecycle events"
      - name_pattern: "refund-*"
        defined_in: internal/events/topics.go
        purpose: "refund processing"
        
  - type: redis
    reference: "idempotency cache"
    config_location: helm/values.yaml
    config_path: "redis.host"
    purpose: "prevent duplicate payment processing"
    
  - type: external_api
    name: Stripe
    reference: "payment processor"
    config_location: helm/values.yaml
    config_path: "stripe.apiUrl"
    client_location: internal/clients/stripe/
```

**.joe/topology.yaml**
```yaml
this_service: payment-service

calls:
  - service: fraud-detector
    protocol: grpc
    purpose: "check transaction for fraud before processing"
    endpoint_defined_in: internal/clients/fraud/client.go
    
  - service: user-service
    protocol: http
    purpose: "fetch user payment methods and billing info"
    endpoint_defined_in: internal/clients/user/client.go

publishes_to:
  - type: kafka
    topic_pattern: "payment-events-v*"
    event_types: [PaymentInitiated, PaymentCompleted, PaymentFailed]
    schema_location: api/events/
    producer_location: internal/events/producer.go

subscribes_to:
  - type: kafka
    topic_pattern: "refund-requested-*"
    handler_location: internal/handlers/refund_handler.go

external_calls:
  - name: Stripe
    client_location: internal/clients/stripe/
    purpose: "process card payments and refunds"

databases:
  - type: postgresql
    reference: "payment transactions database"
    access_pattern: read-write
    orm_location: internal/models/
```

**.joe/flows.yaml**
```yaml
participates_in:
  - flow: checkout
    role: "charge customer payment method"
    position: 3
    receives_from: cart-service
    passes_to: order-service
    
  - flow: refund
    role: "process refund via Stripe"
    triggered_by: kafka/refund-requested
    publishes: kafka/refund-completed
```

**.joe/glossary.yaml**
```yaml
terms:
  idempotency_key:
    meaning: "Unique identifier to prevent duplicate payment processing"
    relevance: "Critical for payment safety"
    implementation: internal/middleware/idempotency.go
    
  payment_intent:
    meaning: "Stripe object representing a payment attempt lifecycle"
    see_also: https://stripe.com/docs/payments/payment-intents
    
  PCI:
    meaning: "Payment Card Industry Data Security Standard"
    relevance: "This service handles card data and must be PCI compliant"
```

---

## Usage

Run this analysis on the current repository and generate the `.joe/` files. Place them in the `.joe/` directory at the repository root.

After generating, the developer should:
1. Review the files for accuracy
2. Commit them to the repository
3. Joe will use these files for instant discovery (no LLM parsing needed)

---

## Notes for AI Assistant

- **Be clear, not strict:** These files will be read by another LLM, not a parser. Clarity matters more than format perfection.
- **Be conservative:** If you're not sure about something, omit it rather than guess wrong.
- **Focus on structure:** Your job is to map the codebase, not understand the business logic deeply.
- **Use patterns:** If you see `values-prod.yaml`, assume there's a prod environment.
- **Follow conventions:** Look for standard patterns (12-factor app, etc.)
- **Note uncertainty:** If something might vary, add a comment explaining your reasoning.
- **Add context:** If a relationship is unusual, explain why you think it exists.
- **Be complete but concise:** Include everything important, but don't pad the files.

### Remember: LLM-to-LLM Communication

You're writing for another LLM to read. That means:
- Natural language comments are fine and helpful
- Variations in YAML structure are okay
- Explaining "why" is as valuable as "what"
- If the standard format doesn't fit, describe it another way

The goal is to save Joe's LLM from having to analyze source code. As long as your output clearly conveys the repository's infrastructure dependencies and relationships, it's good enough.
