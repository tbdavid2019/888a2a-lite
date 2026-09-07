# universal-bridge-distribution Specification

## Purpose
Provides zero-friction single-line cross-host distribution mechanisms for 888a2a-lite, including a Hub-hosted POSIX bootstrap installer script and npm global package tooling.

## Requirements

### Requirement: Hub serves standalone POSIX bootstrap installer script
The Hub SHALL serve an executable POSIX shell script at `GET /install.sh` and the raw bridge Python script at `GET /a2a_bridge.py`. The installer script SHALL support execution via `curl -fsSL https://<hub>/install.sh | bash -s -- <args>`.

#### Scenario: Single-line installer execution
- **WHEN** user executes `curl -fsSL https://<hub>/install.sh | bash -s -- --name "MyAgent" --backend openclaw` on a Linux or macOS host with Python 3.10+
- **THEN** the installer verifies Python 3 availability, downloads `a2a_bridge.py` to `~/.a2a/bin/` or `/usr/local/bin/`, marks it executable, and initiates agent bridge execution

#### Scenario: Single-line service installation
- **WHEN** user provides `--install-service` to the piped installer script
- **THEN** the installer sets up and starts the corresponding systemd user unit on Linux or launchd plist on macOS, reporting service status and logs directory

### Requirement: NPM package distribution exposes global a2a binaries
The repository SHALL provide an npm package definition (`package.json`) defining the `888a2a` package with `bin` entries for `a2a` and `a2a-bridge`.

#### Scenario: NPM global installation
- **WHEN** user executes `npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git` or `npm install -g 888a2a`
- **THEN** the `a2a` and `a2a-bridge` command-line utilities become globally available in the user's terminal environment
