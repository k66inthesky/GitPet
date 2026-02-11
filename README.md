# GitPet (gh-pet)

GitPet is a GitHub CLI extension that turns your GitHub activity into a digital companion living in the Cache.

## Install

```bash
gh extension install <owner>/gh-pet
```

## Commands

```bash
gh pet feed    # Sync recent GitHub activity and update pet stats
gh pet status  # Render the current pet state
gh pet suggest # Ask Copilot for creative commit messages
```

## Copilot CLI Extension (Vercel)

Deploy the Vercel Go handler in `api/handler.go`, then register the endpoint in your Copilot Extension configuration to enable `@gitpet status`.

Request body (minimal):
```json
{ "input": "@gitpet status", "user": { "login": "octocat" } }
```

Optional headers:
- `Authorization: Bearer <token>` or `X-GitHub-Token: <token>` for private activity access.

## Notes

- Pet state is stored at `~/.config/gh/gh-pet.json`.
- `feed` uses your GitHub events (last 7 days) plus local `git status/diff` for Thought Fragments.
