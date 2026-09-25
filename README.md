# cooked

Live 1v1 hot-take debates. Pick a side of the take, get matched with someone who disagrees, argue for three rounds, and let the crowd and an AI judge decide who got cooked. Every debate exports as a vertical clip.

```
“5'10 is short.”
AGREE  Marcus  ███████░░░░░░░░░  Priya  DISAGREE
                 Priya cooked Marcus
```

## How a debate works

1. **Pick a side** of the live take (it rotates every 10 minutes so the crowd concentrates) or any take in the library.
2. **Matchmaking** pairs you with someone on the other side. If nobody bites in 12 seconds, you fight **cookbot**, the AI opponent, so the first user always gets a game.
3. **Three rounds**, 45 seconds and 280 characters per turn. Freeze and your turn is skipped on the transcript.
4. **Spectators vote live** with a tug-of-war bar, and keep voting for 30 seconds after the last turn.
5. **Verdict**: with 3+ votes the crowd decides; otherwise the judge does. The judge also writes the headline ("Priya cooked Marcus") and a roast of each side's debating.
6. **Rating**: Elo, with tiers Chud → NPC → Cooker → Menace → Goat. Bot games move rating half as much.
7. **Clip**: one tap renders a 1080×1920 MP4 in the browser (take, bubbles landing, vote bar swinging, verdict slam) and opens the share sheet.

**Challenge links** let you write your own take and send it to a friend; whoever opens it first argues against you. Every link unfurls as a card with the take, the players and the vote split.

## AI

`internal/ai` has one interface with two implementations:

- **Claude** (when `ANTHROPIC_API_KEY` is set): judge, opponent, and moderator on `claude-opus-5` (override with `COOKED_MODEL`), with server-side refusal fallbacks enabled. Responses are parsed defensively; anything unparseable falls back to local.
- **Local** (no key needed): effort-based judging, a slur/doxxing filter, and a canned opponent that never repeats itself.

Moderation always runs the local filter first, then Claude, and fails open only for the Claude step.

## Safety

18+ gate at signup. Prompts are claims about ideas, not identities; race comparisons and roasts of people's appearance are excluded from the library. Slurs, threats, self-harm encouragement, phone numbers and emails are blocked before they post. Anyone can report a debate. Writes are rate limited per IP.

## Stack

Go standard library HTTP, SQLite (pure Go, WAL), Server-Sent Events, vanilla JS, one binary.

```
internal/elo      ratings and tiers
internal/prompts  the take library and 10-minute rotation
internal/ai       judge, opponent, moderator (Claude + local)
internal/core     users, matches, turns, votes, verdicts (SQLite)
internal/game     matchmaking, bot turns, turn timeouts, judging
internal/hub      live event fan-out per debate and for the lobby
internal/card     1200x630 share cards
internal/web      API, SSE, pages, static app, clip renderer
```

## Run

```sh
make run                                   # http://127.0.0.1:8095, local AI
ANTHROPIC_API_KEY=... make run             # Claude judge, opponent and moderator
make test
```

## Deploy (Fly.io)

```sh
fly launch --no-deploy
fly volumes create cooked_data --size 1
fly secrets set ANTHROPIC_API_KEY=...
fly deploy
```

## Roadmap

- [ ] Voice turns (15-second voice notes, transcribed for the judge)
- [ ] Group rooms: debates only your friend group can see and vote on
- [ ] Daily "take of the day" push notification
- [ ] Review queue for reported debates
- [ ] Server-rendered MP4 clips for browsers without MediaRecorder
