# Alternative Traffic Mirroring & Shadowing Tools

This document provides a comparison of cloneproxy with other available traffic mirroring, shadowing, and duplication tools in the ecosystem.

## Quick Comparison Table

| Tool | Language | Stars | Response Validation | Dynamic Targets | Production Ready | Use Case |
|------|----------|-------|---------------------|-----------------|------------------|----------|
| **cloneproxy** | Go | - | ✅ Yes (SHA1 comparison) | ❌ No | ✅ Yes | Testing/validation with mismatch detection |
| **NGINX** | C | - | ❌ **No** | ⚠️ Config-based | ✅ Yes | General-purpose reverse proxy with mirroring |
| **trafficmirror** | Go | 6 | ❌ No | ✅ Yes (Runtime API) | ⚠️ Unknown | Dynamic traffic mirroring |
| **miffy** | Rust | 10 | ✅ Yes (Kafka diff) | ❌ No | ⚠️ Experimental | Shadow testing with offline analysis |
| **Envoy Proxy** | C++ | 24k+ | ❌ No | ✅ Yes | ✅ Yes | Enterprise service mesh |
| **slow_cooker** | Go | 341 | ⚠️ Partial | ❌ No | ✅ Yes | Load testing with mirroring |
| **octo-proxy** | Go | - | ❌ No | ❌ No | ⚠️ Unknown | TCP/TLS mirroring |
| **caddy-mirror** | Go | - | ❌ No | ❌ No | ⚠️ Module | Caddy 2 plugin |

## Detailed Comparison

---

### 1. cloneproxy (This Project)

**Repository:** `https://github.com/cavanaug/cloneproxy` (fork of `dludwig/cloneproxy`)

**Language:** Go

**Key Features:**
- ✅ HTTP reverse proxy with traffic cloning
- ✅ **Response comparison and validation** (SHA1 hash comparison)
- ✅ Configurable timeout for both target and clone
- ✅ Traffic percentage control (send only X% to clone)
- ✅ Path-based routing and configuration
- ✅ Request body rewriting for target/clone differences
- ✅ Hop counter to prevent circular requests (X-Cloneproxy-Request header)
- ✅ Min TLS version configuration
- ✅ SSL security check skipping per path
- ✅ HTTP request profiling with pprof
- ✅ Detailed logging with structured fields (logrus)
- ✅ Mismatch detection and alerting

**Best For:**
- Testing API changes by comparing production vs. staging responses
- Validating that new implementations produce identical results
- Migration testing (old system vs. new system)
- Detecting regressions through response validation

**Unique Selling Point:**
> **Response validation with mismatch detection** - cloneproxy doesn't just mirror traffic, it actively compares responses from both targets and logs discrepancies. This makes it ideal for validation and testing scenarios.

**Configuration Example:**
```hjson
{
  ListenPort: ":8080"
  LogLevel: 1
  Paths: [
    {
      Path: "/"
      TargetURL: "http://production.example.com"
      CloneURL: "http://staging.example.com"
      ClonePercent: 100
    }
  ]
}
```

---

### 2. NGINX (ngx_http_mirror_module)

**Website:** `https://nginx.org/en/docs/http/ngx_http_mirror_module.html`

**Language:** C

**Availability:** Built into NGINX since version 1.13.4 (July 2017)

**Key Features:**
- ✅ Built-in HTTP request mirroring
- ✅ Fire-and-forget (mirror responses ignored)
- ✅ Multiple mirror destinations supported
- ✅ Traffic percentage control (via split_clients module)
- ✅ Production-tested and battle-hardened
- ✅ Part of widely-deployed NGINX reverse proxy
- ⚠️ **Slow mirrors can throttle original requests**
- ❌ **NO response comparison or validation**
- ❌ **NO mismatch detection or logging**

**Best For:**
- Organizations already using NGINX as reverse proxy
- Simple traffic replication without validation needs
- Logging/auditing of production traffic
- Pre-warming caches on staging environments
- Security analysis via traffic copying

**Critical Limitations:**

1. **No Response Validation** - NGINX mirrors traffic but completely ignores mirror responses. There's no comparison, validation, or mismatch detection. This is fundamentally different from cloneproxy's primary use case.

2. **Slow Mirror Backend Throttles Original Requests** - Mirror subrequests are linked to original requests. If the mirror backend is slow (e.g., 1 second response time), the original request will also be delayed by that amount. This makes NGINX unsuitable for scenarios where the mirror backend is slower or under-resourced.

3. **Configuration Complexity** - Partial traffic mirroring requires combining multiple NGINX modules (split_clients, if statements) which can be error-prone.

**Configuration Example (Basic):**
```nginx
upstream production {
    server prod.example.com:80;
}

upstream staging {
    server staging.example.com:80;
}

server {
    listen 8080;
    
    location / {
        mirror /mirror;
        proxy_pass http://production;
    }
    
    location = /mirror {
        internal;
        proxy_pass http://staging$request_uri;
    }
}
```

**Configuration Example (50% Traffic Mirroring):**
```nginx
# Split traffic 50/50 based on client IP
split_clients $remote_addr $mirror_backend {
    50% staging;
    *   "";
}

server {
    listen 8080;
    
    location / {
        mirror /mirror;
        proxy_pass http://production;
    }
    
    location = /mirror {
        internal;
        
        # Drop request if not selected for mirroring
        if ($mirror_backend = "") {
            return 400;
        }
        
        proxy_pass http://$mirror_backend$request_uri;
    }
}
```

**Configuration Example (API Key-Based Splitting):**
```nginx
# Consistent splitting based on API key
split_clients $arg_apikey $mirror_backend {
    30% staging;
    *   "";
}

server {
    listen 8080;
    
    location / {
        mirror /mirror;
        proxy_pass http://production;
    }
    
    location = /mirror {
        internal;
        if ($mirror_backend = "") {
            return 400;
        }
        proxy_pass http://$mirror_backend$request_uri;
    }
}
```

**Performance Characteristics:**

| Scenario | Impact on Original Requests |
|----------|----------------------------|
| Fast mirror backend (< 10ms) | ✅ Minimal impact (< 1ms overhead) |
| Normal mirror backend (10-100ms) | ⚠️ Noticeable slowdown |
| Slow mirror backend (> 1s) | ❌ **Severe throttling** |
| Mirror backend errors | ✅ No impact (errors ignored) |
| Mirror backend down | ⚠️ Timeout delays until mirror fails |

**Differentiator vs. cloneproxy:**
- **NGINX:** General-purpose reverse proxy with mirroring capability; fire-and-forget; no validation; slow mirrors affect performance
- **cloneproxy:** Purpose-built for testing/validation; compares responses; separate timeouts protect against slow mirrors; detailed mismatch logging

**When to Choose NGINX over cloneproxy:**
- ✅ Already using NGINX for reverse proxying
- ✅ Need to mirror to multiple destinations
- ✅ Don't care about response validation
- ✅ Mirror backend is guaranteed to be fast
- ✅ Want minimal operational overhead (single tool)

**When to Choose cloneproxy over NGINX:**
- ✅ **Response validation is critical** (primary use case)
- ✅ Need mismatch detection and logging
- ✅ Mirror backend might be slow or unreliable
- ✅ Testing API migrations or refactoring
- ✅ Want simpler configuration for testing scenarios
- ✅ Need separate timeout controls

---

### 3. trafficmirror (rb3ckers)

**Repository:** `https://github.com/rb3ckers/trafficmirror`

**Language:** Go

**Stars:** ~6

**Key Features:**
- ✅ Mirrors HTTP traffic to main target + multiple mirror targets
- ✅ **Runtime modifiable mirror targets** via API endpoint
- ✅ Password-protected control endpoint
- ✅ Error handling with retry logic
- ⚠️ No response comparison or validation

**Best For:**
- Dynamic testing environments where mirror targets change frequently
- Load testing by mirroring production traffic to multiple endpoints
- Scenarios requiring runtime target configuration

**Differentiator vs. cloneproxy:**
- **trafficmirror:** Focuses on dynamic target management; can add/remove mirrors at runtime
- **cloneproxy:** Focuses on response validation; static configuration with detailed comparison

---

### 4. miffy (elmarx)

**Repository:** `https://github.com/elmarx/miffy`

**Language:** Rust

**Stars:** ~10

**Key Features:**
- ✅ Shadow-testing proxy with reference/candidate model
- ✅ Reference always wins (returns fast response to client)
- ✅ **Publishes differences to Kafka** for offline analysis
- ✅ Adds `X-Shadow-Test-Role` header to distinguish requests
- ⚠️ More complex setup (requires Kafka)
- ⚠️ Rust toolchain required

**Best For:**
- Large-scale shadow testing with data analytics pipeline
- Organizations already using Kafka for event streaming
- Scenarios requiring offline analysis of discrepancies

**Differentiator vs. cloneproxy:**
- **miffy:** Kafka-based async analysis; better for high-volume scenarios with data science workflows
- **cloneproxy:** Real-time logging; simpler setup, no external dependencies

---

### 5. Envoy Proxy

**Repository:** `https://github.com/envoyproxy/envoy`

**Language:** C++

**Stars:** 24,000+

**Key Features:**
- ✅ Enterprise-grade service mesh proxy
- ✅ **Built-in request mirroring** via `RequestMirrorPolicy`
- ✅ Advanced routing, load balancing, circuit breaking
- ✅ Observability with metrics, tracing, logging
- ✅ gRPC, HTTP/1.1, HTTP/2, WebSocket support
- ⚠️ Complex configuration
- ❌ No built-in response comparison

**Best For:**
- Enterprise microservices architectures
- Organizations adopting service mesh patterns
- Complex routing and traffic management needs

**Differentiator vs. cloneproxy:**
- **Envoy:** Full-featured service mesh; part of larger infrastructure strategy
- **cloneproxy:** Lightweight, focused tool for testing/validation; easier to deploy and configure

**Configuration Example (Envoy):**
```yaml
request_mirror_policies:
  - cluster: shadow_cluster
    runtime_fraction:
      default_value:
        numerator: 100
```

---

### 6. slow_cooker (BuoyantIO)

**Repository:** `https://github.com/BuoyantIO/slow_cooker`

**Language:** Go

**Stars:** 341

**Key Features:**
- ✅ Load testing tool with long-running test support
- ✅ Predictable, constant load generation
- ✅ Lifecycle issue detection (memory leaks, etc.)
- ✅ Good/bad request tracking
- ⚠️ Primarily a load tester, not a proxy
- ⚠️ Limited response comparison features

**Best For:**
- Long-running load tests (hours/days)
- Detecting lifecycle issues in services
- Realistic traffic pattern generation

**Differentiator vs. cloneproxy:**
- **slow_cooker:** Load testing tool; generates traffic from scratch
- **cloneproxy:** Proxy tool; mirrors real production traffic

---

### 7. octo-proxy (nothinux)

**Repository:** `https://github.com/nothinux/octo-proxy`

**Language:** Go

**Key Features:**
- ✅ TCP and TLS proxy with traffic mirroring
- ✅ Works at TCP layer (not just HTTP)
- ⚠️ No HTTP-specific features
- ❌ No response validation

**Best For:**
- TCP-level traffic mirroring
- Database connection mirroring
- Non-HTTP protocols

**Differentiator vs. cloneproxy:**
- **octo-proxy:** TCP layer; works with any protocol
- **cloneproxy:** HTTP layer; deep HTTP inspection and validation

---

### 8. caddy-mirror (dotvezz)

**Repository:** `https://github.com/dotvezz/caddy-mirror`

**Language:** Go (Caddy 2 module)

**Key Features:**
- ✅ Caddy 2 plugin for request mirroring
- ✅ Integrates with Caddy's rich ecosystem
- ⚠️ Requires Caddy server
- ❌ No response validation

**Best For:**
- Organizations already using Caddy
- Simple mirroring needs without validation

**Differentiator vs. cloneproxy:**
- **caddy-mirror:** Caddy plugin; part of Caddy ecosystem
- **cloneproxy:** Standalone service; no dependencies

---

### 9. k8s-service-broadcasting (FUSAKLA)

**Repository:** `https://github.com/FUSAKLA/k8s-service-broadcasting`

**Language:** Go

**Key Features:**
- ✅ Kubernetes-native traffic broadcasting
- ✅ Service-level traffic duplication
- ⚠️ Kubernetes-specific
- ❌ No response validation

**Best For:**
- Kubernetes environments
- Service mesh integration
- Cloud-native architectures

**Differentiator vs. cloneproxy:**
- **k8s-service-broadcasting:** Kubernetes operator; cloud-native
- **cloneproxy:** Standalone binary; works anywhere

---

## Feature Matrix

| Feature | cloneproxy | NGINX | trafficmirror | miffy | Envoy | slow_cooker |
|---------|-----------|-------|---------------|-------|-------|-------------|
| **Response Comparison** | ✅ SHA1 hash | ❌ | ❌ | ✅ Kafka | ❌ | ⚠️ Basic |
| **Mismatch Logging** | ✅ Detailed | ❌ | ❌ | ✅ Kafka | ❌ | ⚠️ Basic |
| **Dynamic Targets** | ❌ | ⚠️ Config | ✅ API | ❌ | ✅ Config | ❌ |
| **Traffic % Control** | ✅ Yes | ✅ split_clients | ⚠️ Unclear | ✅ Yes | ✅ Yes | N/A |
| **Path-based Routing** | ✅ Yes | ✅ Yes | ❌ | ⚠️ Unclear | ✅ Yes | N/A |
| **Request Rewriting** | ✅ Yes | ✅ Yes | ❌ | ❌ | ✅ Yes | ❌ |
| **TLS Support** | ✅ Yes | ✅ Yes | ⚠️ Unclear | ✅ Yes | ✅ Yes | ✅ Yes |
| **Slow Mirror Impact** | ✅ Protected | ❌ **Throttles** | ⚠️ Unknown | ✅ Async | ⚠️ Unknown | N/A |
| **External Dependencies** | None | None | None | Kafka | None | None |
| **Configuration Format** | HJSON | NGINX conf | CLI flags | TOML | YAML | CLI flags |
| **Structured Logging** | ✅ Logrus | ✅ Standard | ⚠️ Basic | ⚠️ Basic | ✅ Advanced | ⚠️ Basic |
| **Multiple Mirrors** | ❌ | ✅ Yes | ✅ Yes | ❌ | ✅ Yes | N/A |

---

## Use Case Recommendations

### Choose **cloneproxy** when you need:
- ✅ Response validation and mismatch detection
- ✅ Testing API migrations or refactoring
- ✅ Verifying staging matches production behavior
- ✅ Simple deployment without external dependencies
- ✅ Detailed logging of discrepancies
- ✅ Protection against slow mirror backends

### Choose **NGINX** when you need:
- ✅ Already using NGINX as your reverse proxy
- ✅ Simple traffic replication (no validation required)
- ✅ Multiple mirror destinations
- ✅ Minimal operational overhead (single tool)
- ✅ Battle-tested production reliability
- ⚠️ **Mirror backend must be fast and reliable**

### Choose **trafficmirror** when you need:
- Runtime modification of mirror targets
- Multiple simultaneous mirror destinations
- Dynamic testing environments

### Choose **miffy** when you need:
- High-volume shadow testing with analytics
- Kafka-based event streaming architecture
- Offline analysis of response differences
- Data science workflows on traffic patterns

### Choose **Envoy** when you need:
- Enterprise service mesh capabilities
- Advanced routing and load balancing
- Full observability stack (metrics, traces, logs)
- gRPC and HTTP/2 support
- Part of larger Istio/service mesh deployment

### Choose **slow_cooker** when you need:
- Load testing, not mirroring
- Long-running performance tests
- Lifecycle issue detection (memory leaks, etc.)

---

## Architecture Patterns

### Real-time Validation (cloneproxy)
```
Client → cloneproxy → Target (production)
              ↓
              └→ Clone (staging)
              ↓
         Compare responses
              ↓
         Log mismatches
```

### Fire-and-Forget Mirroring (NGINX)
```
Client → NGINX → Production → Client
           ↓
           └→ Staging (response ignored)
           
⚠️ Note: Slow staging delays client response!
```

### Async Analysis (miffy)
```
Client → miffy → Reference (production) → Client
           ↓
           └→ Candidate (staging)
           ↓
       Kafka Topic
           ↓
    Analytics Pipeline
```

### Service Mesh (Envoy)
```
Service A → Envoy Sidecar → Service B
                 ↓
                 └→ Mirror (Service B shadow)
```

---

## Migration & Deployment Considerations

| Aspect | cloneproxy | NGINX | Envoy | miffy |
|--------|-----------|-------|-------|-------|
| **Setup Complexity** | Low | Low | High | Medium |
| **Learning Curve** | Low | Medium | High | Medium |
| **Operational Overhead** | Low | Low | Medium-High | Medium |
| **Scalability** | Medium | Very High | Very High | High |
| **Resource Usage** | Low | Low | Medium | Medium |
| **Best Deployment** | VM/Container | Anywhere | K8s/Service Mesh | Container + Kafka |
| **Existing Infrastructure** | None | NGINX | Service Mesh | Kafka |

---

## Conclusion

**cloneproxy's unique value proposition** is its focus on **response validation and mismatch detection**. While many tools can mirror or shadow traffic, cloneproxy actively compares responses and provides detailed logging of discrepancies.

This makes it particularly well-suited for:
- API migration validation
- Regression testing
- Staging/production parity verification
- Gradual rollouts with confidence

**Comparison with alternatives:**
- For simple traffic mirroring without validation, **NGINX** or trafficmirror may suffice
- For enterprise-scale service mesh deployments, **Envoy** is more appropriate
- For high-volume scenarios requiring data analytics, **miffy** with Kafka provides better scalability
- For existing NGINX users wanting basic mirroring, the **ngx_http_mirror_module** is already available

**Critical differentiators:**
1. **Response Validation** - Only cloneproxy and miffy validate responses; NGINX and Envoy don't
2. **Slow Mirror Protection** - cloneproxy has separate timeouts; NGINX throttles original requests
3. **Mismatch Logging** - cloneproxy provides detailed real-time logs; miffy uses Kafka for async analysis

**Choose cloneproxy when response correctness matters more than scale or complexity.**

---

## Additional Resources

- [Original cloneproxy by dludwig](https://github.com/dludwig/cloneproxy)
- [NGINX Mirror Module Documentation](https://nginx.org/en/docs/http/ngx_http_mirror_module.html)
- [NGINX Mirror Module Tips and Tricks](https://alex.dzyoba.com/blog/nginx-mirror/)
- [Envoy Request Mirroring Documentation](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#envoy-v3-api-msg-config-route-v3-routeaction-requestmirrorpolicy)
- [Shadow Testing Pattern (Martin Fowler)](https://martinfowler.com/bliki/ParallelChange.html)
- [Traffic Shadowing Best Practices](https://www.thoughtworks.com/radar/techniques/traffic-shadowing)

---

**Last Updated:** 2026-02-26  
**cloneproxy Version:** master (post-security-fixes)
