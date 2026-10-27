# Changelog

## [0.7.1](https://github.com/vilosource/herddash/compare/v0.7.0...v0.7.1) (2026-10-26)


### Documentation

* **install:** a new binary starts about 30 s late under launchd on macOS ([#59](https://github.com/vilosource/herddash/issues/59)) ([0412d97](https://github.com/vilosource/herddash/commit/0412d97532f0ac650f3e6a3eaaa56daa0f69b8f3))

## [0.7.0](https://github.com/vilosource/herddash/compare/v0.6.0...v0.7.0) (2026-10-26)


### Features

* **board:** a title bar with a text logo and a summary (Story 5.19, ADR-0013) ([86d61de](https://github.com/vilosource/herddash/commit/86d61de85d5080103ad61083e89e3409e280c50d))

## [0.6.0](https://github.com/vilosource/herddash/compare/v0.5.0...v0.6.0) (2026-10-26)


### Features

* **engine:** retry a Profile lookup that failed transiently (Story 5.4) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))


### Bug Fixes

* **board:** exact Box text and one group per unlisted workspace (Story 5.13) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **board:** notice a hung Daemon, a skipped publication and a rejected event ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **core:** a local-command stderr result is not a turn; bash mode stays one ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **core:** a manual /recap reaches the Board, and local commands are not turns ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **core:** a resumed Claude Code session's stopped tasks close (Story 5.2) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **core:** a resumed subagent counts as background work (Story 5.16) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **engine:** a new Agent in a reused pane starts fresh ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **engine:** a Profile hold always ends (Story 5.18) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **engine:** a released pane's next Agent starts fresh across a restart ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* **engine:** treat repeated snapshot failures as a lost connection (Story 5.12) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))


### Documentation

* **adr:** ADR-0012, an observable keepalive on the event stream ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))
* retrospectives for Epics 1 to 4, and documents agents read match the code (Story 5.8) ([4b3564e](https://github.com/vilosource/herddash/commit/4b3564eee5b598918e3124039fd219577b7fb45b))

## [0.5.0](https://github.com/vilosource/herddash/compare/v0.4.0...v0.5.0) (2026-10-22)


### Features

* **core:** order each workspace group Finished, Idle, Background, Working, newest first ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** background tasks and subagents from Claude Code transcripts (ADR-0009) ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** Claude Code's session recap on the Board (ADR-0010) ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** mark Agents waiting on background work as Background ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** Needs you from Claude Code's session record (ADR-0008) ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **httpapi:** traffic-light colours: red needs you, amber idle, green working ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))


### Bug Fixes

* **core:** a TaskStop result ends its background task ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **core:** end a turn on stop_sequence ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** clamp Claude's waiting stamp to the previous Transition, not the read time ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** harden the session watch after review ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **engine:** read an Agent's children 250 ms after its turn ends, not 1 s ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))
* **httpapi:** reload the Board when the Daemon behind it restarts ([0a500d5](https://github.com/vilosource/herddash/commit/0a500d5cf2506635b5484d0ba4b74c70f1a2874c))

## [0.4.0](https://github.com/vilosource/herddash/compare/v0.3.0...v0.4.0) (2026-10-21)


### Features

* **httpapi:** group the Board by workspace, Needs you alone on top ([#51](https://github.com/vilosource/herddash/issues/51)) ([0903245](https://github.com/vilosource/herddash/commit/090324514fa1a3081e857cc24d02c6efe1c0f993))

## [0.3.0](https://github.com/vilosource/herddash/compare/v0.2.0...v0.3.0) (2026-10-21)


### Features

* **httpapi:** lead each Box with workspace and tab, state at the bottom ([#49](https://github.com/vilosource/herddash/issues/49)) ([c659675](https://github.com/vilosource/herddash/commit/c6596755cda11d59a204b5ab3929f3761e7b3342))

## [0.2.0](https://github.com/vilosource/herddash/compare/v0.1.0...v0.2.0) (2026-10-21)


### Features

* **httpapi:** put workspace and tab second in each Box ([#47](https://github.com/vilosource/herddash/issues/47)) ([35087b0](https://github.com/vilosource/herddash/commit/35087b0540ee93c4d8f6ef435f7d5623e6578e37))

## 0.1.0 (2026-10-21)


### Features

* **config:** configuration, data directory and socket discovery (Story 1.2) ([#15](https://github.com/vilosource/herddash/issues/15)) ([c714931](https://github.com/vilosource/herddash/commit/c7149318445ab556ad660a3295984de50d807684))
* **core:** each Agent's view, Context Line and Board order (Story 3.1) ([#32](https://github.com/vilosource/herddash/issues/32)) ([8fbb240](https://github.com/vilosource/herddash/commit/8fbb240c846156fcfe1ecf83ac776481ac46407c))
* **deploy:** launchd agent and systemd user unit with an install guide (Story 4.2) ([#44](https://github.com/vilosource/herddash/issues/44)) ([829af67](https://github.com/vilosource/herddash/commit/829af674b5b466087e075dcfebbc57600ec46626))
* **engine:** publish immutable views and change lists (Story 3.2) ([#33](https://github.com/vilosource/herddash/issues/33)) ([a8c990f](https://github.com/vilosource/herddash/commit/a8c990fb3466712c8632474d611ed77fa42127d7))
* **engine:** reconnect forever and detect herdr restarts by epoch (Story 1.6) ([#21](https://github.com/vilosource/herddash/issues/21)) ([a3838be](https://github.com/vilosource/herddash/commit/a3838bed98d96b1f812f879a26a4ca974af991c4))
* **engine:** record every Transition (Story 2.2) ([#27](https://github.com/vilosource/herddash/issues/27)) ([b72f192](https://github.com/vilosource/herddash/commit/b72f1923f0f659281926dcf961c12afe260d3416))
* **engine:** record gaps as resync markers (Story 2.3) ([#28](https://github.com/vilosource/herddash/issues/28)) ([f8ffd3f](https://github.com/vilosource/herddash/commit/f8ffd3f733e3fea3a2ae83d2e9080db8164a660b))
* **engine:** refuse unsupported herdr, with a dogfooding override (Story 1.8) ([#24](https://github.com/vilosource/herddash/issues/24)) ([9ab1b5b](https://github.com/vilosource/herddash/commit/9ab1b5b8b31c8d27f4278d94fb700defaab9004b))
* **engine:** resolve the Profile each Agent runs under (Story 1.9) ([#25](https://github.com/vilosource/herddash/issues/25)) ([0f38886](https://github.com/vilosource/herddash/commit/0f38886fbd06ecb217d8846461ad5bfe5a28c6ce))
* **engine:** resync when the stream cannot be trusted (Story 1.7) ([#23](https://github.com/vilosource/herddash/issues/23)) ([88fb175](https://github.com/vilosource/herddash/commit/88fb175434dbce0e185c915dc9c0874c7258c628))
* **engine:** Waiting Time that survives restarts (Story 2.4) ([#29](https://github.com/vilosource/herddash/issues/29)) ([46008e4](https://github.com/vilosource/herddash/commit/46008e42dc57a38816be2b9f03ffc835b0e5096d))
* **engine:** watch every Agent's Status live with herddash serve (Story 1.5) ([#20](https://github.com/vilosource/herddash/issues/20)) ([85f1ed2](https://github.com/vilosource/herddash/commit/85f1ed2d712912578b5a30e47096523b8dee17d0))
* **herdr:** milestone-1 findings against herdr 0.9.5 and generated wire types (Story 1.3) ([#18](https://github.com/vilosource/herddash/issues/18)) ([2e4ba49](https://github.com/vilosource/herddash/commit/2e4ba49e3f8c847e18a11d148b16810d9153919e))
* **herdr:** socket client mechanics (Story 1.4) ([#19](https://github.com/vilosource/herddash/issues/19)) ([f1bfaf5](https://github.com/vilosource/herddash/commit/f1bfaf58587a956d3f461869d427f2dc0db2af1d))
* **httpapi:** connection and version strips on the Board (Story 3.9) ([#41](https://github.com/vilosource/herddash/issues/41)) ([6fce275](https://github.com/vilosource/herddash/commit/6fce27516c15fbd89ae02dcd2659206895ec0670))
* **httpapi:** history per Agent (Story 3.5) ([#36](https://github.com/vilosource/herddash/issues/36)) ([84b8da4](https://github.com/vilosource/herddash/commit/84b8da4b1cf4230b4c6bea1f81b04951b70e0961))
* **httpapi:** live event stream (Story 3.4) ([#35](https://github.com/vilosource/herddash/issues/35)) ([d64182a](https://github.com/vilosource/herddash/commit/d64182a1c2b1224586f1b91f5c9079f4dcf1ea7d))
* **httpapi:** read-only API on loopback with current state (Story 3.3) ([#34](https://github.com/vilosource/herddash/issues/34)) ([dfe94fe](https://github.com/vilosource/herddash/commit/dfe94feed1ffffa2ad46f9cc2d2ee50a588757a3))
* **httpapi:** the Board renders every Agent (Story 3.7) ([#39](https://github.com/vilosource/herddash/issues/39)) ([b55474c](https://github.com/vilosource/herddash/commit/b55474c968d92f4988dc4568905c1713bd198864))
* **httpapi:** the Board stays live (Story 3.8) ([#40](https://github.com/vilosource/herddash/issues/40)) ([6198ea8](https://github.com/vilosource/herddash/commit/6198ea80bfb4fa7d47f494cc3af3b0bdbf147c70))
* **importer:** import the plugin's log as history (Story 2.6) ([#31](https://github.com/vilosource/herddash/issues/31)) ([378459b](https://github.com/vilosource/herddash/commit/378459bff640b523bdf2588ec7bd3f07ce01dddd))
* project skeleton, enforced import rules and CI (Story 1.1) ([#13](https://github.com/vilosource/herddash/issues/13)) ([08acd87](https://github.com/vilosource/herddash/commit/08acd8778ad83c943a34048b7a186f088a6b85a9))
* **release:** build and attach release binaries for every Host (Story 4.3) ([#45](https://github.com/vilosource/herddash/issues/45)) ([3dbe1ba](https://github.com/vilosource/herddash/commit/3dbe1ba67ee6187cd5ee05faca24466f18fd5fac))
* **store:** a local database herddash owns (Story 2.1) ([#26](https://github.com/vilosource/herddash/issues/26)) ([f5710db](https://github.com/vilosource/herddash/commit/f5710dbf8dcd45e94e8723b5a0c821580cfcf8b3))
* **store:** Retention keeps the database bounded (Story 2.5) ([#30](https://github.com/vilosource/herddash/issues/30)) ([ffab32c](https://github.com/vilosource/herddash/commit/ffab32cac9891bb8054a8833474082845994f256))


### Bug Fixes

* **ci:** let release-please open its pull request, and not at 1.0.0 ([081b836](https://github.com/vilosource/herddash/commit/081b8368aa727be6942882ef3e7b1435cef5758f))


### Documentation

* accept ADR-0001 as option B and accept ADR-0002 ([#4](https://github.com/vilosource/herddash/issues/4)) ([8632901](https://github.com/vilosource/herddash/commit/86329017fc8d319dc45a1e9aea8779f599e953dc))
* **architecture:** finalize the architecture spine and ADRs 0004 to 0007 ([#7](https://github.com/vilosource/herddash/issues/7)) ([762d448](https://github.com/vilosource/herddash/commit/762d448be235550f5e7d714ae035217e85a3e9d1))
* **claude:** describe the live herdr on 0.9.5 and the code that now exists ([#38](https://github.com/vilosource/herddash/issues/38)) ([9ec89df](https://github.com/vilosource/herddash/commit/9ec89df2d08f75aadc9714ece0c6b1bd5ca6b697))
* **design:** Board visual design, approved by the operator (Story 3.6) ([#37](https://github.com/vilosource/herddash/issues/37)) ([843dd1b](https://github.com/vilosource/herddash/commit/843dd1bce4ddee2d36b142cd4957d49b366240c2))
* **epics:** break the PRD and architecture into four epics and 27 stories ([#9](https://github.com/vilosource/herddash/issues/9)) ([4c5465d](https://github.com/vilosource/herddash/commit/4c5465d148a10a77e5781d8b5d3e409daa244df0))
* evaluate alecuba16/herdr-webui and settle the scorecard ([f8a3dcb](https://github.com/vilosource/herddash/commit/f8a3dcb8c0eff0276a4c2cf40d631ca4c545cd44))
* evaluate devswha/herdr-web-ui against this project's thesis ([890447a](https://github.com/vilosource/herddash/commit/890447aacad0e61079157e8767f9b93150ca6759))
* **prd:** finalize the PRD rework, record ADR-0003 ([#5](https://github.com/vilosource/herddash/issues/5)) ([944e95d](https://github.com/vilosource/herddash/commit/944e95d801c18a9196383e6222b479dd8fce191c))
* record the herdr socket API findings and correct the PRD ([00ad131](https://github.com/vilosource/herddash/commit/00ad1310e055103c1393c1db25a6b2f0b5ad72d5))
* separate first-hand findings from relayed ones in the herdr research ([be30089](https://github.com/vilosource/herddash/commit/be30089bf29604a4efe0e1d723f606e42302efc0))
* **sprint:** generate the sprint plan and open the build ([#11](https://github.com/vilosource/herddash/issues/11)) ([184609e](https://github.com/vilosource/herddash/commit/184609e42c1bb12acef49f2b7cb0e6c740289efb))
* stop CLAUDE.md asserting a commit count that goes stale ([5e1be15](https://github.com/vilosource/herddash/commit/5e1be157bd456b242366fb38bf12d12c962ed1c5))
