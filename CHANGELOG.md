## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* native MCP client integration ([cda80a2](https://github.com/atakang7/axon/commit/cda80a249af41bce16f6cd4c91db4fc25c8d88f7))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* allow agent to construct pruner internally from model ([715af80](https://github.com/atakang7/axon/commit/715af80f4b8c732331e09dfe96b25ce1866aa332))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))
* update CONTRIBUTING.md to reflect flattened architecture ([1329c78](https://github.com/atakang7/axon/commit/1329c78c28f3df91b8105accf93a08bca6c817be))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* native MCP client integration ([cda80a2](https://github.com/atakang7/axon/commit/cda80a249af41bce16f6cd4c91db4fc25c8d88f7))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))
* update CONTRIBUTING.md to reflect flattened architecture ([1329c78](https://github.com/atakang7/axon/commit/1329c78c28f3df91b8105accf93a08bca6c817be))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* native MCP client integration ([cda80a2](https://github.com/atakang7/axon/commit/cda80a249af41bce16f6cd4c91db4fc25c8d88f7))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))
* update CONTRIBUTING.md to reflect flattened architecture ([1329c78](https://github.com/atakang7/axon/commit/1329c78c28f3df91b8105accf93a08bca6c817be))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* native MCP client integration ([cda80a2](https://github.com/atakang7/axon/commit/cda80a249af41bce16f6cd4c91db4fc25c8d88f7))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* native MCP client integration ([cda80a2](https://github.com/atakang7/axon/commit/cda80a249af41bce16f6cd4c91db4fc25c8d88f7))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync architecture docs with monolithic engine philosophy ([e9ca67f](https://github.com/atakang7/axon/commit/e9ca67fef1b6c9dbff5921a07629cd74e7bbf48b))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* flatten repository into a monolithic domain package ([72cf0f1](https://github.com/atakang7/axon/commit/72cf0f12d8c06fdd00c6b10d3f7f62f5493fbe0b))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* expose PrunerConfig to embedders ([f3c9688](https://github.com/atakang7/axon/commit/f3c9688c6a6ef2cee5a871027497cb3ab34469aa))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* enforce strict json tool-calling and separate reasoning block ([2467dd7](https://github.com/atakang7/axon/commit/2467dd731236393218dd13d4857865b2703b020f))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **llm:** revert XML fallback parser to maintain strict minimalism ([b86e672](https://github.com/atakang7/axon/commit/b86e6724b125bf14d7cf94cd98307d8eb4681cb8))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** implement manual XML fallback parser for broken OpenRouter tool calls ([2d86385](https://github.com/atakang7/axon/commit/2d863850845590c6a4e38643cc5a65fe81452d2f))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** append critical reminder to pruner prompt to prevent context hijacking ([7402c50](https://github.com/atakang7/axon/commit/7402c502f3fc36d7756085ab7ec1af1cd2a6ec09))
* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** remove global include_reasoning and capture reasoning_content explicitly ([5649e77](https://github.com/atakang7/axon/commit/5649e77fe11ce20b023011168f61907ab6846ac0))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **agent:** increase pruner max tokens for reasoning models ([a9034d8](https://github.com/atakang7/axon/commit/a9034d8d0bb57f386b57d65297bcc3ff9ce0c433))
* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **llm:** force include_reasoning for openrouter to prevent missing JSON schemas ([104275c](https://github.com/atakang7/axon/commit/104275c4e077995112870a7c468f705249ee9caf))
* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **llm:** omit tools payload when empty to prevent silent provider failures ([f6c8449](https://github.com/atakang7/axon/commit/f6c8449837480cc49185c6b2bbab4f9f71a2ae8e))
* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [2.0.0](https://github.com/atakang7/axon/compare/v1.0.3...v2.0.0) (2026-08-07)


### ⚠ BREAKING CHANGES

* **agent:** New rejects a Config.Tools entry with no Name, no Schema
or no Fn instead of accepting it. A caller relying on a tool being
constructed without one of those now gets ErrInvalidTool at New.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **session:** tools.Workspace.RecordEdit takes (path, before) instead
of (path, before, after), and session.Edit no longer has an After field.
An embedder with its own Workspace implementation drops the argument.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* **tools:** read no longer accepts mode=skeleton, search no longer
accepts mode=trace, and exec no longer accepts a mode field at all
(command becomes a required parameter). Callers relying on verify's
auto-detection must pass the build command explicitly.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
* publish the vocabulary so an embedder can read its documentation
* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model
* remove the runtime's own configuration and prompt enrichment
* **agent:** make the model a port instead of a hardwired client
* **pruner:** reduce the curator to a single verb

### Features

* **agent:** allow bounding the built-in toolset, and fix Undo lying to the model ([b6bfff8](https://github.com/atakang7/axon/commit/b6bfff8be08c3ecd6524fcc990a4bae7640b3d48))
* **agent:** make the model a port instead of a hardwired client ([d69621a](https://github.com/atakang7/axon/commit/d69621a9c3c463b0383e4abeb68cbd712a6ac34d))
* **agent:** reject a malformed tool at construction ([b31d7f7](https://github.com/atakang7/axon/commit/b31d7f7542b12e716cb3a7f386441dd39e9f3094))
* publish the vocabulary so an embedder can read its documentation ([066d46f](https://github.com/atakang7/axon/commit/066d46fefbeebaba908a2d9b7ee1d6dcbb3a46ce))


### Bug Fixes

* **llm:** report the whole provider error body, not its first line ([96693d2](https://github.com/atakang7/axon/commit/96693d22ab6f5b481676b2ffebaf1f27c48f7f06))
* **pruner:** report blocks the curator named but could not be parked ([bebaad1](https://github.com/atakang7/axon/commit/bebaad1bc72f8c3de808402fd8373453e9dc152d))
* **session:** keep the session file where the embedder put it on Reset ([6ae439c](https://github.com/atakang7/axon/commit/6ae439c723a2720051e4bb2ffcfb49edb6a25402))
* three hangs and a swallowed interrupt found in review ([802bed1](https://github.com/atakang7/axon/commit/802bed1ed2acaba3f94df51f3266cc260b8d347e))


### Performance

* **session:** bound the undo ledger ([b38e14c](https://github.com/atakang7/axon/commit/b38e14c9c29f9a94f41ef71f781864ba1b6fbc09))
* **session:** stamp block IDs at load, not on every ensure ([8ad513b](https://github.com/atakang7/axon/commit/8ad513b71deac9591f88f0e27557ae477fec80ef))
* **tools:** seek past the backlog when reading a shell log ([3a61f9a](https://github.com/atakang7/axon/commit/3a61f9ae82b946f4cac7cfb141601356214b002e))


### Refactoring

* **agent:** resolve tool limits once at construction instead of at call depth ([c96c589](https://github.com/atakang7/axon/commit/c96c589a291345c65f7963f4c3fae677faaf1489))
* **config:** extract path and limit resolution into internal/config ([0ba36f5](https://github.com/atakang7/axon/commit/0ba36f5d7693e3efd0412ea9b393164874e74db1))
* **llm:** move the model layer into internal/llm behind ToolSpec ([8b36b9e](https://github.com/atakang7/axon/commit/8b36b9e119c2337824f577f83db02ea331823dae))
* **llm:** read the SSE stream on the calling goroutine ([37e359f](https://github.com/atakang7/axon/commit/37e359f8bf163e33483b8292ecccd875f18c1385))
* **pruner:** reduce the curator to a single verb ([60acad9](https://github.com/atakang7/axon/commit/60acad9283be0e37da466131d6a0b52e01e8c73f))
* remove the runtime's own configuration and prompt enrichment ([b919072](https://github.com/atakang7/axon/commit/b919072e5697e942273a321aa1906c9df1bd4fa9))
* **session:** keep only pre-edit contents in the undo ledger ([d5cc939](https://github.com/atakang7/axon/commit/d5cc939bf2fd673350201a47fbbf3f007713aeda))
* **session:** move conversation state into internal/session ([34dccd7](https://github.com/atakang7/axon/commit/34dccd769538fc0bf5cbb002a0c636c463cf14bc))
* **tools:** drop the formatter dispatch table and unify the rg runner ([d8ed9ee](https://github.com/atakang7/axon/commit/d8ed9ee205f1c47102f578a4a8b3975b04ed9d34))
* **tools:** drop the modes that guess at the language ([e18c0ef](https://github.com/atakang7/axon/commit/e18c0ef5053dbe80d0e031dc0990b774f0fb7b52))
* **tools:** move tools into internal/tools behind narrow capabilities ([16be7df](https://github.com/atakang7/axon/commit/16be7df976d57737b84d8d2cdf38a858b8533454))
* **tools:** stop defending against a nil context ([b7a4a6a](https://github.com/atakang7/axon/commit/b7a4a6a3d8228cdb630408f94fb4e971a8b4bdf5))


### Documentation

* correct false invariants and make the layering rule executable ([4fb5122](https://github.com/atakang7/axon/commit/4fb51224540bcdc746b7574588b3b41a32abdd57))
* stop overclaiming what the layering test enforces ([7f15713](https://github.com/atakang7/axon/commit/7f15713e06bbee39d2ca1828504cc49290b45d15))
* sync ARCHITECTURE with the current API ([35d3407](https://github.com/atakang7/axon/commit/35d3407223cbac3ccd69a2a1d2fce2dce8d19856))

## [1.0.3](https://github.com/atakang7/axon/compare/v1.0.2...v1.0.3) (2026-08-07)


### Refactoring

* **internals:** eliminate cluttered logic in bg, pruner, and agent ([48f1022](https://github.com/atakang7/axon/commit/48f1022803e7d69ffefdcc944427ae4246c1ab6b))

## [1.0.2](https://github.com/atakang7/axon/compare/v1.0.1...v1.0.2) (2026-08-07)


### Refactoring

* **api:** deconstruct monolithic api.go into semantic modules ([1d62914](https://github.com/atakang7/axon/commit/1d6291478af978ca7159fa5d521d78b84a2b618d))
* **tools:** isolate ExecTool logic into semantic functions ([21f8c62](https://github.com/atakang7/axon/commit/21f8c62d7365f6fa14fa4c7cb129bb005f843608))
* **tools:** simplify ReadTool with parseAndValidateReadInput ([9ea6a22](https://github.com/atakang7/axon/commit/9ea6a226851dbba4ee90c4a7c47c867e56fddaab))
* **tools:** simplify WriteTool with parseAndValidateWriteInput ([71c62bc](https://github.com/atakang7/axon/commit/71c62bc0ae6c3857b6c7457c8ca7d1d315780c84))

## [1.0.1](https://github.com/atakang7/axon/compare/v1.0.0...v1.0.1) (2026-08-07)


### Refactoring

* apply GCMF principles to memory, session, and llm components ([59e78bc](https://github.com/atakang7/axon/commit/59e78bc4507fea611eafe166a44731de62c42997))

## [1.0.0](https://github.com/atakang7/axon/compare/v0.4.3...v1.0.0) (2026-05-19)


### ⚠ BREAKING CHANGES

* github.com/atakang7/axon/cmd/axon is gone. Replace
"go install github.com/atakang7/axon/cmd/axon@latest" with
"go install github.com/atakang7/bouton/cmd/bouton@latest". The runtime
import path github.com/atakang7/axon/agent is unchanged.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>

### Features

* drop bundled CLI; axon is now library-only ([69e2026](https://github.com/atakang7/axon/commit/69e20268278087c6a71ba41c8dca4a3c4d145de0))

## [0.4.3](https://github.com/atakang7/axon/compare/v0.4.2...v0.4.3) (2026-05-19)


### Bug Fixes

* **ci:** narrow semantic-release BREAKING-CHANGE keywords ([ffe6a57](https://github.com/atakang7/axon/commit/ffe6a570c7d5e788c2909d756d1a4dcb447884f8))

## [0.4.2](https://github.com/atakang7/axon/compare/v0.4.1...v0.4.2) (2026-05-19)


### Bug Fixes

* **ci:** drop Windows from goreleaser matrix ([fd401bf](https://github.com/atakang7/axon/commit/fd401bf14bfa861f99cd32a19f1b5022786ca326))

## [0.4.1](https://github.com/atakang7/axon/compare/v0.4.0...v0.4.1) (2026-05-19)


### ⚠ BREAKING CHANGES

* syntax, and the automated release flow.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>

### Bug Fixes

* **ci:** verify automated release pipeline end-to-end ([0670412](https://github.com/atakang7/axon/commit/0670412ac7edcfd9e0229fa6f0de01820cd8e26b))


### CI

* automate releases on every push to main ([4572ce3](https://github.com/atakang7/axon/commit/4572ce3bd7295102d40c1e6707b8a14072db8d99))

# Changelog

All notable changes to axon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://www.semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-05-19

### Added

- **Public Go library API.** The runtime is importable as `github.com/atakang7/axon/agent`. Surface: `Config`, `New`, `Step`, `Run`, `Interrupt`, `Reset`, `Undo`, `Cd`, `Session`, `Close`, `SessionPath`.
- **`Config.OnEvent`** — plain `func(ctx, Event)` field for observability. The runtime emits structured `Event`s with a `Kind` discriminator (`KindToken`, `KindToolCall`, `KindToolResult`, `KindTurnEnd`, `KindPruneStart`/`End`, ...). Fan-out is whatever the embedder writes inside the closure; no Handler interface, no MultiHandler ceremony.
- **Sentinel errors:** `ErrNoProvider`, `ErrNoSystemPrompt`, `ErrToolNotFound`, `ErrDuplicateTool`, `ErrInterrupted`.
- **CLI exports:** `DataDir`, `ConfigDir`, `ProvidersPath`, `SessionPath`, `EnvString`, `ApplyProviderEnvOverrides`, `ProviderNames` — small surface so CLIs can resolve XDG paths without re-implementing them.
- **`examples/minimal/main.go`** — the 30-line embed.

### Changed

- **Repository structure flipped:** runtime is `agent/`; reference CLI is `cmd/axon/`. The `internal/` boundary that made the runtime un-importable is gone.
- **CLI shell moved out of the runtime.** `Main()`, the interactive provider picker, `lastChoice` persistence, the YAML loader, `customtool.go`, `ui.go`, and the `pasteAwareInput` reader all live in `cmd/axon` now. The runtime no longer writes to stdout — all output goes through `Config.OnEvent`.
- **Slash commands are CLI-only.** `/new`, `/undo`, `/cd`, `/pwd`, `/session` live in `cmd/axon/commands.go` and map onto methods on `*Agent`.
- **`Config.SystemPrompt` is required.** The runtime has no opinion of its own about what an agent is; the role text is the embedder's call. CLI ships a small default-prompt string for its own use.

### Removed

- `agent.Main()` — replaced by `New` + `Step`/`Run`.
- `agent.BuildTools` — `New` does the composition.
- `agent.NewBare`, `agent.Builtins`, `Config.DisableBuiltins`, YAML `disable_builtins` — built-ins are unconditional. One constructor.
- `agent.Handler` interface, `HandlerFunc`, `MultiHandler`, `DiscardHandler` — replaced by `Config.OnEvent`. Composition is a closure.
- JSONL event log and `--log-json` flag. Embedders who want structured logs write 5 lines of `OnEvent` that delegate to `slog` or anything else.
- `defaultRolePrompt` — the runtime no longer ships a coding-agent personality.
- Direct `ui*` and `logger.Emit` calls from the runtime.
- All test files. They referenced the pre-refactor types and will be reintroduced against the new API in a follow-up.
- **`park`, `recall`, `forget`, `refresh` as model-facing tools.** Park / Recall / Forget are now `Session` methods driven by the secondary-LLM pruner, not tools the model invokes. `refresh` is gone entirely. The current built-in tool set is: `read`, `write`, `exec`, `bash_output`, `kill_shell`, `search`, `task`.
- **REALITY CHECK / ANCHORING PASS / MOMENTUM BEAT prompt regime.** The system prompt is now a thin role + built-in tool catalog + probes + project orientation; see `agent/prompt.go`.

## [0.3.0] - 2026-05-07

### Added

- TTL-based memory management with auto-parking
- Enhanced system prompt with REALITY CHECK requirement
- Tool surface: `read`, `write`, `exec`, `search`, `task`, `park`, `recall`, `forget`, `refresh`, `bash_output`, `kill_shell`
- Structured turn lifecycle with mandatory verification
- Dashboard showing active blocks, TTL counts, parked blocks, and current task
- Aggressive context triage with immediate forgetting of raw data
- Pruner component for automatic context management (fires at ~10K tokens)

### Updated

- System prompt now requires REALITY CHECK before any tool call
- Memory system replaced archive/retrieve with TTL-based park/recall/forget/refresh
- README updated with current tool descriptions and examples
- Enhanced guidance for proper prompt usage and turn discipline

### Core loop (Updated)

The agent follows a strict turn lifecycle:

1. **REALITY CHECK** - Must output GOAL, CONSTRAINTS, ACTION, TRASH before any tool
2. **ANCHORING PASS** - Full six-slot ATTENTION block (GOAL, STATE, HISTORY, CONSTRAINTS, MOVES, DIMENSION)
3. **MOMENTUM BEAT** - Three-line reasoning between tool calls (DELTA, DRIFT, NEXT)
4. **GATHER & COMMUNE** - Bundle task + first action if requirements clear
5. **EXECUTE & VERIFY** - One code change per turn, mandatory verification
6. **PURGE CONTEXT** - Forget raw data immediately, manage TTL pressure
7. **DELIVER & HALT** - Memory tools last, silent completion

## [0.2.0] - 2025-01-15

### Added

- Session persistence with automatic resume
- File edit undo functionality (`/undo`)
- Multiple provider support (OpenAI, Claude, Ollama, LM Studio)
- Environment variable overrides for provider config

### Changed

- Moved from custom prompts to structured tool definitions

## [0.1.0] - 2024-12-01

### Added

- Initial release
- Basic tool loop: read, write, exec, search
- OpenAI-compatible API support
