# Email

Email is optional. With it, a parent who forgets their password gets a reset link, and each parent can get a summary of the children's week on Sunday evening. Without it, `zigrt reset-password <email>` in the container sets a new password, and there is no weekly email.

Zigrt sends plain-text mail only, with no images or tracking, and nothing else leaves the server.

## Settings

| Variable | Chart | Default | Meaning |
| --- | --- | --- | --- |
| `ZIGRT_SMTP_HOST` | `smtp.host` | none | The SMTP server; email is off without it |
| `ZIGRT_SMTP_SECURITY` | `smtp.security` | `starttls` | `starttls` (port 587), `tls` (465) or `none` (25, only for a relay on the same host or network) |
| `ZIGRT_SMTP_PORT` | `smtp.port` | by security | The port, if not the usual one |
| `ZIGRT_SMTP_USERNAME` | `smtp.username` | none | Sign-in name, if the server needs one |
| `ZIGRT_SMTP_PASSWORD` | `smtp.existingSecret` | none | Its password. Use `ZIGRT_SMTP_PASSWORD_FILE` or, in the chart, a Secret |
| `ZIGRT_SMTP_FROM` | `smtp.from` | none | The sender, like `Zigrt <zigrt@example.com>` |
| `ZIGRT_BASE_URL` | `baseURL` | none | Needed: the links in emails start with it |

Zigrt refuses to start when a setting is wrong, naming each one, so a typo shows at once rather than at the first email.

With the chart, keep the password in a Secret you create, never in values:

```sh
kubectl -n zigrt create secret generic zigrt-smtp --from-literal=password='...'
```

```yaml
smtp:
  host: smtp.fastmail.com
  username: you@example.com
  from: "Zigrt <you@example.com>"
  existingSecret: zigrt-smtp
```

Most providers want an app password, not your account password.

## What is sent

- **Password reset:** a link that works once, for an hour. Asking again within five minutes sends nothing new. The page answers the same whether or not the address belongs to a parent.
- **Weekly summary:** on Sunday from 18:00 in the household's time zone, each child's XP against the week's goal, days the goal was met, streak, new blocks, anything stuck on or found hard. A parent turns it off under **Household → Weekly email**; it is on for everyone else. If the server is off all Sunday evening, that week's summary is skipped.

## When mail doesn't arrive

A failed send is retried after 1, 5 and 30 minutes, 2 hours and 12 hours, then given up. The log says `email failed` with the server's answer each time. With [metrics](install.md#metrics) on, `zigrt_email_outbox{state="failed"}` counts messages given up on. Sent mail is forgotten after 30 days.
