# 7. Students sign in by picking their name and typing a PIN

Date: 2026-11-16 · Status: accepted

## Context

Students are five to eleven years old and share family devices. They cannot be expected to remember a username or type a password.

## Decision

The signed-out home page shows a tile per student. Choosing one leads to a large number pad for a 4 to 6 digit PIN. The student's ID in the URL is not a secret. Wrong PINs are throttled per account with growing delays, and ten in a row lock the student until a parent unlocks them. Parents sign in with an email and password and are throttled but never locked. A student session lasts 30 days on a device and a parent session 7, each sliding with use.

## Consequences

- Anyone who can reach the app can see the students' first names. Zigrt is meant to sit behind the home network or a proxy, and student records hold nothing but a name, an avatar tone and a PIN hash.
- A sibling can lock another student out by guessing; a parent unlocks in one tap.
- The PIN page is a plain form with a real field, so it works without JavaScript; the pad is an enhancement.
