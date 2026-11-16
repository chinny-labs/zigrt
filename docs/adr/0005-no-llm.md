# 5. No LLM at runtime

Date: 2026-11-16 · Status: accepted

## Context

An LLM could generate explanations or act as a tutor. It would also make the app depend on a network service, send children's work off the home network, and give answers that cannot be checked in advance.

## Decision

Zigrt uses no LLM. Problems are generated from templates with a seed; explanations and worked solutions are authored and reviewed in the curriculum.

## Consequences

- The app works offline and makes no outbound requests.
- Every problem can be regenerated exactly from its template ID and seed, so any attempt can be replayed.
- All teaching quality comes from the curriculum, which is the largest piece of work in the project.
