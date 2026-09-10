# Repository Guide

## Overview

This repository contains a small Go Discord bot built with `discordgo`.

## Structure

- `cmd/bot/main.go`: executable entry point.
- `internal/app`: application lifecycle and feature registration.
- `internal/config`: environment and secret-file configuration.
- `internal/discord`: Discord session wrapper and gateway intents.
- `internal/feature/<name>`: independent bot features and event handlers.

## Commands

- Run tests with `go test ./...`.
- Run static checks with `go vet ./...`.
- Build the bot with `go build ./cmd/bot`.
- Format changed Go files with `gofmt`.

## Conventions

- Add new behavior as a package under `internal/feature` and register it in `internal/app/app.go`.
- Prefer the Go standard library and do not add third-party dependencies only for convenience.
- Add an external dependency only when implementing the same behavior with the standard library would be substantially more complex or error-prone, as is the case with `discordgo`.
