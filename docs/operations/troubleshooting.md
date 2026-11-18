# Troubleshooting

## Setting up

**I can't find the setup token.** It is printed when the server starts with no parent account, and a restarted server prints a new one. Take it from the running container's log:

```sh
kubectl logs -n zigrt deploy/zigrt | grep -i "setup token"
docker compose logs zigrt | grep -i "setup token"
```

**Sign-in goes straight back to the sign-in page.** Session cookies are `Secure`, so the browser must be on HTTPS. Put Zigrt behind a TLS proxy, or for a plain-HTTP trial on your LAN set `ZIGRT_INSECURE_COOKIES=true` ([Install](install.md#tls)).

**The logs show the proxy's address for every request.** Set `ZIGRT_TRUSTED_PROXIES` (chart: `trustedProxies`) to the proxy's or ingress controller's address range.

## Students

**A student is locked out.** Ten wrong PINs in a row lock a student. Unlock them from their page, or set a new PIN in their settings.

**A student is asked about things they already know.** Their course includes the earlier blocks it builds on. Run the placement check, or **Start at a grade** ([Using Zigrt](../guide/parents.md#choosing-where-a-child-starts)).

**A student keeps getting reviews and few new lessons.** After a break, while more than ten reviews are due, a student does five reviews before each new lesson instead of three ([How Zigrt teaches](../guide/how-it-works.md#reviews)). A daily goal a little higher for a week clears it faster.

**A lesson keeps stopping.** After it stops twice, Zigrt reviews the blocks it is built on before offering it again, and you get a notification. Sitting with the child for the worked example often helps. You can also mark the block placed or not from their settings.

## Parents

**I forgot my password.** With email set up, use **Forgot your password?** on the sign-in page. Otherwise another parent can't see passwords either; run `zigrt reset-password you@example.com` in the container and it prints a new one.

**Email doesn't arrive.** See [Email](email.md#when-mail-doesnt-arrive).

## The server

**`/readyz` answers 503.** It says why: usually the content folder holds a pack that needs a newer Zigrt. Upgrade the app or remove that pack file ([Content packs](content.md)).

**A pack I added isn't used.** The log says `content loaded` with the pack that is running, and warns about any file it skipped (one that doesn't load, or another curriculum's).

**Something looks wrong with a student's progress.** `zigrt rebuild` recomputes every student's progress from the history. It is safe to run at any time.
