## ADDED Requirements

### Requirement: Universal bridge runtime supports secretary role and charter caching
The `a2a-bridge` runtime SHALL provide `--role` (e.g. `--role=secretary`), `--charter-cache-dir` (defaulting to `~/.a2a/groups/`), and `--auto-minutes` flags. When launched, the bridge SHALL automatically synchronize the group charter upon receiving task streams and provide cognitive hooks for meeting minutes distillation.

#### Scenario: Bridge launched with secretary role
- **WHEN** user executes `a2a bridge --role=secretary --group=<groupId> --auto-minutes`
- **THEN** the bridge establishes connection as group secretary, automatically caches the group charter, and starts recording substantive decisions

#### Scenario: Automatic charter caching across offline restarts
- **WHEN** the bridge restarts after being offline
- **THEN** the bridge verifies its cached `charter_version` against the Hub before executing prompt assembly, refreshing cache if stale
