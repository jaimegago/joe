# Collaboration with LLMs: My History

Date: 2026-02-16

This document is a personal record of the ideas, architectural decisions, and standards I contributed to Joe. It is written for my own reference so I can remember what my part was and why it mattered.

## Foundational Architecture Decisions

### Two-binary architecture (joe + joecored)
I designed the original "two binaries" approach: a local interactive client (joe) and a daemonized core (joecored) connected by a clean HTTP API. This separation shaped Joe into a reliable operator system instead of a monolith.

Impact:
- Clear responsibility boundaries: the user-facing agent stays lightweight and fast, while the core handles long-running refreshes and infrastructure state.
- Safer operations: local tooling and core tooling can be governed differently without ambiguity.
- Easier evolution: new adapters and capabilities are added to the core without breaking the user workflow.

### .joe directory in repos
I introduced the idea of a .joe directory in infrastructure repos to capture structured operational context.

Impact:
- Gives Joe a consistent, discoverable surface for environment knowledge and intent.
- Enables scalable onboarding across multiple repos without bespoke configuration.
- Bridges human tribal knowledge and machine reasoning through a predictable convention.

## Operating Model and Tooling Strategy

### Go-native diagnostics tools
I decided to implement standard SRE troubleshooting tools using Go native packages (e.g., traceroute-style, ping-like probes, HTTP request checks).

Impact:
- Reduces external dependencies and makes Joe portable across environments.
- Improves reliability, since tools share a consistent codebase and error handling.
- Allows precise safety controls and logging for each operation.

### LLM provider agnostic design
I set the requirement that Joe be model-provider agnostic from day one.

Impact:
- Avoids vendor lock-in and keeps the system resilient to API or pricing changes.
- Lets teams adopt the best model for their security and performance needs.
- Enables experimentation with minimal friction.

### Runtime model switching (/model)
I drove the idea for switching models at runtime via the /model command.

Impact:
- Enables live experimentation with different models in the same session.
- Helps teams balance cost and quality without restarting or reconfiguring.
- Creates a flexible developer workflow when models evolve quickly.

## Testing and Validation

### Realistic lab environment design
I designed a complex lab setup with multi-cloud infrastructure and LLM-based error generation to test Joe in conditions close to real-world operations.

Impact:
- Enables realistic, repeatable testing across diverse environments.
- Surfaces integration issues earlier by simulating real failure patterns.
- Improves confidence that Joe behaves correctly under complex conditions.

## Security and Safety

### Security in layers
I promoted the "security in layers" concept as the core defensive strategy.

Impact:
- Establishes safety as a system property, not a feature bolt-on.
- Makes it clear where protections live (policy, tool gating, invariants).
- Scales to new adapters without redefining trust boundaries each time.

### Safety around LLMs
I emphasized explicit safety around LLM-driven actions, especially around mutation and execution.

Impact:
- Prevents inadvertent destructive actions by requiring clear tiers and policies.
- Builds user trust by making risk levels visible and enforced.
- Aligns the tool with real-world operator expectations.

## Engineering Standards and Quality

### Choosing Go for Joe
I decided to use Go as the implementation language and encouraged the team to embrace its strengths.

Impact:
- Strong standard library makes it ideal for infrastructure and networking tools.
- Concurrency model (goroutines, channels) fits background refresh loops and agent orchestration.
- Single static binaries simplify distribution and deployment in ops environments.
- Predictable performance and low memory overhead suit long-running daemons.
- Rich ecosystem for cloud SDKs and observability.

### Go standards for LLMs
I defined the Go standards that LLMs should follow while contributing code.

Impact:
- Keeps generated code consistent and maintainable.
- Ensures error handling and interfaces are designed at point of use.
- Reduces review overhead and improves confidence in code quality.

### Focus on test coverage
I insisted on a strong focus on test coverage, especially for core logic.

Impact:
- Makes refactors safer as the system evolves.
- Enables rapid adapter expansion with confidence.
- Creates a shared expectation of reliability across the codebase.

## Summary

My contributions established the system architecture, its safety posture, and the engineering discipline that keeps Joe reliable as it grows. The core idea has always been to build an AI infrastructure copilot that is safe, modular, and durable over time. This document captures the key decisions I made toward that goal.
