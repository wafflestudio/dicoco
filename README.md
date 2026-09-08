# Discord Bot

디스코드 Bot 레포

## Requirements

- Go 1.27.1
- Discord bot token

Discord Developer Portal에서 봇의 **Message Content Intent**를 활성화해야 합니다.

## Project structure

```text
cmd/bot/                    봇 실행 진입점
internal/
├── app/                    설정, 기능 등록, 실행 및 종료 흐름
├── config/                 실행 환경 설정 로딩
├── discord/                discordgo 세션을 감싼 클라이언트
└── feature/                서로 독립적인 봇 기능
    └── ping/               멘션을 통한 봇 응답 확인
```
