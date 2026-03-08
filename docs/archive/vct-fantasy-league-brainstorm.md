# VCT Fantasy League — Brainstorm Notes

Archived from belayer instance `vct-fantasy-league` (pre-rebuild).

## Initial Brainstorm Q&A

**Objective:** Build a fantasy league UI for Valorant Champions Tour (VCT) esports using https://github.com/axsddlr/vlrggapi for data.

**Q: What frontend framework and tech stack?**
A: TanStack Start with full TanStack utilities (Query, etc.)

**Q: Is there a backend/database needed?**
A: Yes, simple backend and user accounts to persist fantasy teams.

**Q: What is the scoring system?**
A: Research stats available on VLR and the API, see what they mean, award combat performance not just kills. Determine an algorithm and document it. Build it so we can easily tweak the points/formula afterwards.

**Q: What is the fantasy team roster structure?**
A: Positional slots for the 5 roles (Duelist, Initiator, Controller, Sentinel, IGL) plus a wildcard as a 6th.

**Q: What is the draft mechanism?**
A: Auction with salary cap.

**Q: What is the competition format?**
A: Total points leaderboard with ability to reset for specific international events.

**Q: Which VCT tournaments/regions are in scope?**
A: All of them.

**Q: What happens when a player's real team is eliminated?**
A: Waiver/replacement mechanic.

**Q: User authentication?**
A: Required. Find something with a solid free tier for Google or Discord sign-in.

**Q: Existing design assets?**
A: No, reverse-engineered from Valorant's public visual identity.

---

## Research Notes

### Data Source (vlrggapi)
- REST API hosted at vlrggapi.vercel.app, no auth, 600 req/min rate limit
- Endpoints:
  - `/v2/stats` — player leaderboards
  - `/v2/player` — profile + per-agent stats
  - `/v2/match/details` — per-map per-player stats
  - `/v2/team` — roster with roles
  - `/v2/events` + `/v2/events/matches` — tournament data
- Available stats: ACS, K/D/A, KAST%, ADR, KPR, APR, FKPR, FDPR, HS%, clutch%, multi-kills (2K-5K), plants, defuses, Rating 2.0
- Limitations: no search-by-name, no structured brackets, need vlr.gg numeric IDs, no real-time websocket updates

### Valorant Roles (affects scoring balance)
- **Duelists**: entry fraggers, high kills/first kills
- **Initiators**: intel/setup, high assists
- **Controllers**: map control via smokes, high KAST%
- **Sentinels**: defensive anchors, high survival rates

### Fantasy Scoring (reference: DraftKings)
- Kill +2, Assist +1, Combat Score +0.02/pt
- Clutch bonuses: 1v2 +1, 1v3 +2, 1v4 +3, 1v5 +5
- Multi-kill bonuses: 3K +0.25, 4K +1.5, 5K (ace) +3
- Match sweep +5, round not played +0.5
- Deaths NOT penalized in DraftKings (consider whether we should penalize)

### Design Language (Valorant-themed)
- Colors: Red #FF4655, Dark BG #0F1923 / #1A242E, Off-white text #ECE8E1, Dark red #BD3944
- Fonts: Tungsten Bold (headlines), DIN Next (UI), Montserrat (body)
- Style: Angular/geometric shapes, high-contrast, dark-dominant, military-precision aesthetic

---

## Planned Deliverables

1. **Data Layer & API Integration** — Service layer for vlrggapi integration, caching strategy (stay under rate limits), mapping vlr.gg IDs to internal player/team models. Pre-fetch and cache player/team directories to address lack of search.

2. **Fantasy Scoring System Design** — Scoring rules balanced across roles so Duelists don't always dominate. Consider: death penalties, role-specific bonuses, first blood bonuses, KAST bonuses. Reference DraftKings but make it our own.

3. **Fantasy Team Rules** — Team building constraints:
   - Roster size and composition (5 roles + 1 wildcard)
   - Maximum 2 players from any single real team
   - Budget/salary cap system (auction draft)
   - Captain/star player multiplier
   - Waiver/replacement for eliminated teams
   - Trade mechanics

4. **Tournament Tracking** — Track active tournaments, map eliminated teams, lock/unlock fantasy rosters around match days. Handle VCT season structure (Kickoff -> Masters -> League Stages -> Champions).

5. **UI/UX Design System** — Valorant-themed component library: color tokens, typography scale, angular card components, stat display widgets, leaderboard tables, team builder interface. Dark theme with red accents.

6. **Core Pages/Features**:
   - Dashboard/home with live scores and standings
   - Team builder/draft interface
   - Player browser with stats and filters
   - Matchday view with live scoring
   - League standings and head-to-head
   - Player detail pages with historical stats
   - Settings/league management

7. **Tech Stack**: TanStack Start + TanStack Query, TypeScript, auth with Google/Discord (free tier)
