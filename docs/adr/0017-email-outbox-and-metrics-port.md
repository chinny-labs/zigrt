# 17. Email through an outbox; metrics on their own port

Date: 2026-11-17 · Status: accepted

## Context

Plan sections 2, 7.5 and 10 ask for optional email (password reset, a weekly summary on Sunday evening) and an optional Prometheus endpoint, off by default. Goldberry's ADR 7 settled email for a sibling app: an outbox in the database, sent by an in-process worker.

## Decision

- **An outbox.** A message is a row in `email_outbox`, written in the transaction that decides to send it. A job every minute delivers what is due over SMTP and retries a failure after 1, 5 and 30 minutes, 2 hours, then 12 hours, giving up after six tries. A dedupe key per message means a catch-up or retry never sends twice. Messages older than 30 days are dropped.
- **Plain text only.** No HTML, images or tracking; links use `ZIGRT_BASE_URL`, which is required when SMTP is set.
- **Password reset links** are 256-bit tokens, stored hashed, one hour, single use. Using one ends every session and uses up the parent's other links. "Forgot your password?" answers the same whether or not the address is a parent's, and sends at most one link per parent every five minutes. Reset paths are kept out of the request log. Without SMTP, `zigrt reset-password` remains the way.
- **The weekly email** goes on Sunday from 18:00 household time. It holds each child's XP against the week's goal, days the goal was met, streak, new blocks, a lesson stuck on, topics found hard and weeks without practice. It is sent once per week (a job-run record). A Sunday evening when the server was off is skipped, not sent late. Each parent can turn theirs off on the Household page.
- **SMTP:** STARTTLS (587) by default, implicit TLS (465), or none for a relay on the same network. The password may come from a file or Secret (`ZIGRT_SMTP_PASSWORD_FILE`, chart `smtp.existingSecret`). Neither the outbox nor reset links are exported: they mean nothing in another install.
- **Metrics on a separate listener** (`ZIGRT_METRICS_LISTEN`), not a path on the app's port, so an ingress routing to the app never publishes them. They cover requests and latency by route pattern, job results, email results and the outbox backlog, students and active students, and build and pack info. The chart adds the port and an optional ServiceMonitor.

## Consequences

- An SMTP outage delays mail and shows in metrics; it never fails a request or loses a message silently.
- Email is the only outbound traffic Zigrt can make, and only when configured (plan §10.2).
- A skipped Sunday means no summary that week, which is simpler than deciding what "last week" means on a Tuesday.
