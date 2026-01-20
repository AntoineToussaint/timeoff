# DevOps, Security & Production Readiness

> **Summary:** This system is at POC/prototype stage (30% production-ready). Critical gaps: no authentication/authorization (all endpoints open), no encryption at rest, no Row-Level Security (employees can see others' data), no observability (metrics, tracing, structured logging), no resilience patterns (retries, circuit breakers), no health checks or graceful shutdown. Before production: implement JWT/OAuth2 auth, add RLS policies in PostgreSQL, encrypt PII, add Prometheus metrics + OpenTelemetry tracing, implement health endpoints, add rate limiting. The current SQLite store is for local dev only—production needs PostgreSQL with proper connection pooling. Deployment should use Kubernetes with horizontal pod autoscaling, secrets management via Vault/AWS Secrets Manager.

---

## Executive Summary

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  PRODUCTION READINESS SCORE                                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Security        ████░░░░░░  40%   - Auth missing, no encryption at rest   │
│  Observability   ██░░░░░░░░  20%   - No metrics, no tracing                │
│  Resilience      ███░░░░░░░  30%   - No retries, no circuit breakers       │
│  Scalability     ████░░░░░░  40%   - No caching layer, needs optimization  │
│  Operations      ██░░░░░░░░  20%   - No health checks, no graceful shutdown│
│                                                                             │
│  OVERALL         ███░░░░░░░  30%   - Prototype stage, not production-ready │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Part 1: Security

### 1.1 Authentication & Authorization

#### Current State: ❌ None

The API has no authentication. Anyone can access any endpoint.

#### TODO: Authentication

| Priority | Item | Description |
|----------|------|-------------|
| P0 | API Authentication | JWT or OAuth2 for API access |
| P0 | Session Management | Secure session handling with proper expiry |
| P1 | Service-to-Service Auth | mTLS or API keys for internal services |
| P1 | SSO Integration | SAML/OIDC for enterprise customers |
| P2 | API Key Management | For programmatic access with rotation |

#### TODO: Authorization

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Role-Based Access Control | Employee, Manager, HR Admin, System Admin |
| P0 | Resource-Level Permissions | Users can only see their own data |
| P0 | Row-Level Security (RLS) | Database-enforced tenant/user isolation |
| P1 | Approval Workflows | Manager can only approve direct reports |
| P1 | Policy-Level Permissions | Who can create/modify policies |
| P2 | Audit Role | Read-only access to all data for compliance |

#### Row-Level Security Strategy

```
RLS Implementation (PostgreSQL):
─────────────────────────────────────────────────────────────────────────

Purpose:
  Database-enforced security that prevents data leakage even if 
  application code has bugs. Defense in depth.

Policies to implement:
  • employees: Users see only their own record (or team if manager)
  • transactions: Users see only their own transactions
  • policy_assignments: Users see only their assignments
  • requests: Users see own requests, managers see team requests

Benefits:
  • Security at database layer (not just application)
  • Works even with direct database access
  • Audit-friendly (policies are declarative)
  • Multi-tenant ready
```

#### Authorization Matrix (Target State)

```
┌──────────────────┬──────────┬─────────┬──────────┬────────────┐
│ Action           │ Employee │ Manager │ HR Admin │ Sys Admin  │
├──────────────────┼──────────┼─────────┼──────────┼────────────┤
│ View own balance │    ✓     │    ✓    │    ✓     │     ✓      │
│ View team balance│    ✗     │    ✓    │    ✓     │     ✓      │
│ View all balances│    ✗     │    ✗    │    ✓     │     ✓      │
│ Submit request   │    ✓     │    ✓    │    ✓     │     ✓      │
│ Approve request  │    ✗     │    ✓    │    ✓     │     ✓      │
│ Create policy    │    ✗     │    ✗    │    ✓     │     ✓      │
│ Assign policy    │    ✗     │    ✗    │    ✓     │     ✓      │
│ Trigger rollover │    ✗     │    ✗    │    ✗     │     ✓      │
│ View audit logs  │    ✗     │    ✗    │    ✓     │     ✓      │
│ System config    │    ✗     │    ✗    │    ✗     │     ✓      │
└──────────────────┴──────────┴─────────┴──────────┴────────────┘
```

### 1.2 Data Security

#### Current State: ⚠️ Minimal

- No encryption at rest
- No PII handling strategy
- No data classification

#### TODO: Data Protection

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Encryption at Rest | Database-level encryption (PostgreSQL TDE, AWS RDS encryption) |
| P0 | TLS Everywhere | HTTPS for all endpoints, no exceptions |
| P1 | PII Identification | Tag fields containing personal data |
| P1 | Data Retention Policy | How long to keep transaction history |
| P2 | Right to Erasure | GDPR compliance for data deletion |
| P2 | Data Export | User can export their own data |

#### Data Classification

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  DATA SENSITIVITY LEVELS                                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  🔴 CONFIDENTIAL (encrypt, audit all access)                               │
│     • Employee personal information                                         │
│     • Medical leave reasons                                                 │
│     • Salary-related adjustments                                            │
│                                                                             │
│  🟠 INTERNAL (encrypt, log access)                                         │
│     • Balance information                                                   │
│     • Request history                                                       │
│     • Policy assignments                                                    │
│                                                                             │
│  🟢 PUBLIC (no special handling)                                           │
│     • Policy definitions (rules, not assignments)                           │
│     • System configuration (non-sensitive)                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 API Security

#### Current State: ⚠️ Basic

- No rate limiting
- No input validation framework
- No CORS restrictions in production

#### TODO: API Hardening

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Rate Limiting | Per-user and per-IP limits |
| P0 | Input Validation | Strict schema validation on all inputs |
| P0 | CORS Configuration | Whitelist allowed origins |
| P1 | Request Size Limits | Prevent payload attacks |
| P1 | SQL Injection Prevention | Parameterized queries (verify current state) |
| P2 | Request Signing | HMAC for webhook callbacks |
| P2 | API Versioning | Support graceful deprecation |

### 1.4 Audit & Compliance

#### Current State: ⚠️ Partial

- Transactions are immutable (good)
- No actor tracking on transactions
- No access logging

#### TODO: Audit Trail

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Actor Tracking | Who performed each action |
| P0 | Access Logging | Log all data access, not just writes |
| P1 | IP/Session Tracking | Where requests originated |
| P1 | Change History | Track policy and assignment changes |
| P2 | Compliance Reports | SOC2, GDPR audit exports |
| P2 | Anomaly Detection | Flag unusual patterns |

---

## Part 2: API Design

### 2.1 Current API Assessment

#### Strengths
- RESTful design
- Consistent URL structure
- JSON throughout

#### Weaknesses
- No versioning
- No pagination
- Inconsistent error responses
- No HATEOAS/discoverability

### 2.2 API TODO List

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Error Response Standard | Consistent error format with codes |
| P0 | Pagination | All list endpoints must paginate |
| P1 | API Versioning | `/api/v1/` prefix |
| P1 | OpenAPI Spec | Auto-generated documentation |
| P1 | Idempotency Headers | Client-provided idempotency keys |
| P2 | Bulk Operations | Batch endpoints for efficiency |
| P2 | Webhooks | Event notifications for integrations |
| P2 | GraphQL | Consider for complex queries |

### 2.3 Proposed Error Response Standard

```
Error Response Structure:
─────────────────────────────────────────────────────────────────────────

{
  "error": {
    "code": "INSUFFICIENT_BALANCE",
    "message": "Cannot request 5 days, only 3 available",
    "details": {
      "requested": 5,
      "available": 3,
      "policy_id": "pto-standard"
    },
    "request_id": "req-abc123",
    "documentation_url": "https://docs.example.com/errors/INSUFFICIENT_BALANCE"
  }
}

Error Codes:
─────────────────────────────────────────────────────────────────────────

4xx Client Errors:
  INVALID_INPUT          - Malformed request
  VALIDATION_FAILED      - Business rule violation
  INSUFFICIENT_BALANCE   - Not enough balance
  DUPLICATE_REQUEST      - Idempotency key reused
  RESOURCE_NOT_FOUND     - Entity/policy doesn't exist
  UNAUTHORIZED           - Authentication required
  FORBIDDEN              - Insufficient permissions
  CONFLICT               - Concurrent modification

5xx Server Errors:
  INTERNAL_ERROR         - Unexpected server error
  SERVICE_UNAVAILABLE    - Temporary outage
  DATABASE_ERROR         - Persistence failure
```

### 2.4 Pagination Strategy

```
Pagination Approach:
─────────────────────────────────────────────────────────────────────────

Cursor-based (recommended for transactions):
  GET /api/v1/transactions?cursor=abc123&limit=50
  
  Response includes:
  {
    "data": [...],
    "pagination": {
      "next_cursor": "def456",
      "has_more": true
    }
  }

Offset-based (acceptable for small datasets):
  GET /api/v1/employees?offset=100&limit=50
  
  Response includes:
  {
    "data": [...],
    "pagination": {
      "total": 500,
      "offset": 100,
      "limit": 50
    }
  }
```

---

## Part 3: Performance Considerations

### 3.1 Current Bottlenecks

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  PERFORMANCE HOTSPOTS                                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Balance Calculation                                                     │
│     • Loads ALL transactions for period                                     │
│     • No caching layer                                                      │
│     • N+1 queries for multi-policy                                          │
│                                                                             │
│  2. No Async Processing                                                     │
│     • Reconciliation runs synchronously                                     │
│     • Accrual calculation blocks requests                                   │
│                                                                             │
│  3. Database Optimization Needed                                            │
│     • Missing composite indexes                                             │
│     • No query plan analysis                                                │
│     • Connection pool tuning required                                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 Performance TODO

| Priority | Item | Status | Description |
|----------|------|--------|-------------|
| P0 | Database Indexes | ✅ Done | Composite indexes on all hot paths |
| P1 | Balance Caching | TODO | Cache computed balances with TTL |
| P1 | Snapshots | TODO | Periodic balance snapshots to reduce calculation |
| P2 | Read Replicas | TODO | Separate read/write traffic |
| P2 | Background Jobs | TODO | Async reconciliation and reporting |
| P2 | Connection Pooling | TODO | Optimize database connections |

#### Required Indexes (Add Now)

```
Indexes to create immediately:
─────────────────────────────────────────────────────────────────────────

1. Balance calculation queries:
   • (entity_id, policy_id, effective_at)
   
2. Day uniqueness validation:
   • (entity_id, resource_type, effective_at) WHERE tx_type IN (...)
   
3. Request tracking:
   • (reference_id)
   
4. Entity-wide queries:
   • (entity_id, resource_type, effective_at)

These are zero-risk, immediate performance wins.
```

### 3.3 Scaling Strategy

```
Scale Path:
─────────────────────────────────────────────────────────────────────────

Phase 1: Vertical (up to ~50K employees)
  • Single PostgreSQL instance
  • In-memory caching (local)
  • Sufficient for most companies

Phase 2: Horizontal Reads (up to ~500K employees)
  • PostgreSQL with read replicas
  • Redis for distributed cache
  • Load balancer for API servers

Phase 3: Sharded (500K+ employees)
  • Shard by company/tenant ID
  • Each shard is independent
  • Cross-shard queries via aggregation service

Phase 4: Event-Sourced (enterprise scale)
  • Kafka for transaction log
  • CQRS pattern
  • Materialized views for queries
```

---

## Part 4: Observability

### 4.1 Current State: ❌ None

No metrics, no tracing, no structured logging.

### 4.2 Observability TODO

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Structured Logging | JSON logs with request IDs |
| P0 | Health Endpoints | `/health/live` and `/health/ready` |
| P1 | Metrics | Prometheus metrics for key operations |
| P1 | Distributed Tracing | OpenTelemetry integration |
| P2 | Dashboards | Grafana dashboards for operations |
| P2 | Alerting | PagerDuty/OpsGenie integration |

### 4.3 Key Metrics to Track

```
Metrics Categories:
─────────────────────────────────────────────────────────────────────────

Business Metrics:
  • requests_submitted_total (by status, resource_type)
  • requests_approved_total
  • balance_calculations_total
  • reconciliations_processed_total

Performance Metrics:
  • request_duration_seconds (histogram)
  • balance_calculation_duration_seconds
  • database_query_duration_seconds
  • cache_hit_ratio

Error Metrics:
  • errors_total (by type, endpoint)
  • validation_failures_total
  • database_errors_total

Saturation Metrics:
  • active_connections
  • database_pool_usage
  • memory_usage_bytes
  • goroutines_count
```

---

## Part 5: Deployment

### 5.1 Deployment Options

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  DEPLOYMENT OPTIONS                                                         │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Option A: Container (Recommended)                                          │
│  ├── Docker image with multi-stage build                                    │
│  ├── Kubernetes or ECS for orchestration                                    │
│  ├── Managed PostgreSQL (RDS, Cloud SQL)                                    │
│  └── Best for: Most production deployments                                  │
│                                                                             │
│  Option B: Serverless                                                       │
│  ├── AWS Lambda or Cloud Functions                                          │
│  ├── API Gateway for routing                                                │
│  ├── Serverless PostgreSQL (Aurora Serverless, Neon)                        │
│  └── Best for: Variable load, cost optimization                             │
│                                                                             │
│  Option C: Platform-as-a-Service                                            │
│  ├── Heroku, Render, or Railway                                             │
│  ├── Managed database add-ons                                               │
│  ├── Simplified operations                                                  │
│  └── Best for: Small teams, fast iteration                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Deployment TODO

| Priority | Item | Description |
|----------|------|-------------|
| P0 | Dockerfile | Multi-stage build for minimal image |
| P0 | Health Checks | Liveness and readiness probes |
| P0 | Graceful Shutdown | Handle SIGTERM properly |
| P1 | Helm Chart | Kubernetes deployment configuration |
| P1 | CI/CD Pipeline | Automated testing and deployment |
| P1 | Database Migrations | Versioned schema changes |
| P2 | Blue-Green Deployments | Zero-downtime releases |
| P2 | Feature Flags | Gradual rollout capability |

### 5.3 Environment Configuration

```
Environment Variables (Required):
─────────────────────────────────────────────────────────────────────────

DATABASE_URL        - PostgreSQL connection string
JWT_SECRET          - Token signing key (generate securely)
ENVIRONMENT         - development | staging | production

Environment Variables (Optional):
─────────────────────────────────────────────────────────────────────────

PORT                - Server port (default: 8080)
LOG_LEVEL           - debug | info | warn | error
CACHE_TTL           - Balance cache TTL in seconds
RATE_LIMIT_RPS      - Requests per second limit
CORS_ORIGINS        - Allowed CORS origins (comma-separated)
```

---

## Part 6: Operational Runbooks

### 6.1 Incident Response

```
Runbook: High Error Rate
─────────────────────────────────────────────────────────────────────────

1. Check health endpoints
   - /health/live should return 200
   - /health/ready indicates dependencies

2. Check recent deployments
   - Rollback if error spike correlates with deploy

3. Check database connectivity
   - Connection pool exhaustion?
   - Slow queries?

4. Check external dependencies
   - Auth provider available?
   - Cache service healthy?

5. Escalation
   - Page on-call if not resolved in 15 minutes
```

### 6.2 Maintenance Operations

```
Runbook: Trigger Manual Rollover
─────────────────────────────────────────────────────────────────────────

When: End of fiscal year, policy changes

Steps:
1. Schedule maintenance window
2. Notify affected users
3. Create database backup
4. Execute rollover via admin API
5. Verify balance calculations
6. Monitor for errors
7. Communicate completion

Rollback:
- Restore from backup
- Reverse reconciliation transactions
```

---

## Appendix: Security Checklist

### Pre-Production Checklist

- [ ] Authentication implemented and tested
- [ ] Authorization matrix enforced
- [ ] Row-Level Security (RLS) policies enabled
- [ ] All endpoints require authentication (except health)
- [ ] Rate limiting configured
- [ ] Input validation on all endpoints
- [ ] SQL injection testing passed
- [ ] XSS prevention verified
- [ ] CORS properly configured
- [ ] TLS certificates valid
- [ ] Secrets not in code or logs
- [ ] Database encrypted at rest
- [ ] Database indexes optimized
- [ ] Audit logging enabled
- [ ] Penetration test completed
- [ ] Security review by external party

### Compliance Considerations

```
Compliance Requirements:
─────────────────────────────────────────────────────────────────────────

SOC 2:
  • Access controls
  • Audit trails
  • Encryption
  • Incident response plan

GDPR:
  • Data minimization
  • Right to access
  • Right to erasure
  • Data portability
  • Consent management

HIPAA (if medical leave data):
  • PHI encryption
  • Access logging
  • Business associate agreements
```
