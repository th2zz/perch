# Changelog

All notable changes to Perch are documented in this file.

## [0.6.1] - 2026-11-26

Initial open-source release of Perch (Android client, Go host bridge, `herdr` plugin, and optional push relay).

### Highlights
- **Attention Queue**: Receive real-time notifications when coding agents running under `herdr` block for approval or input; approve, reject, or answer directly from Android.
- **Session Screen & Real Terminal**: Full-screen terminal view backed by `xterm.js` with pinch zoom, 13px minimum readable font floor, vertical/horizontal panning, keyboard-aware input tracking, and non-invasive viewing that preserves your computer's pane dimensions.
- **Host Shell**: Open a standalone PTY shell on your computer directly from the phone with automatic reconnect.
- **Multi-Host & Multi-Agent Support**: Monitor and control sessions across local and SSH-connected `herdr` hosts, start new workspaces/sessions, and inspect structured conversation transcripts and token usage.
- **Host Bridge**: Single-binary Go daemon (`perch-bridge`) with `systemd --user` (Linux/WSL) and `launchd` (macOS) service installation.

### Platform Support Note
- Verified on **WSL2** (host) and **Android 16** (phone).
- Native Linux, macOS, and Android 13–15 (`minSdk 33`) are supported in principle (cross-compilation and unit/plist checks pass) but have not yet been verified on physical hardware.
