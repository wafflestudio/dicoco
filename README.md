# Discord Bot

디스코드 Bot 레포

## Requirements

- Go 1.27.1
- Discord bot token

Discord Developer Portal에서 봇의 **Message Content Intent**를 활성화해야 합니다.

## Development

Go 파일은 커밋할 때 자동으로 `gofmt`를 적용하도록 Git 훅을 제공합니다. 클론한 뒤 한 번만 아래 명령을 실행하세요.

```sh
git config core.hooksPath .githooks
```

훅은 스테이징된 `.go` 파일만 포매팅하고, 변경된 결과를 다시 스테이징합니다.

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

## Structure (멘션 ping 예시)

```text
cmd/bot/main.go (main)
  -> internal/app/app.go (app.Run)
       ├─> internal/config/config.go (config.Load) : 봇 토큰 로드
       ├─> internal/discord/client.go (discord.NewClient) : Intents 설정 및 세션 초기화
       ├─> internal/feature/ping/ping.go (ping.New / Register) : onMessageCreate 핸들러 등록
       └─> internal/discord/client.go (discordClient.Open) : 디스코드 웹소켓 연결 시작

  [ 유저가 디스코드에 메시지 게시 ]

  -> Discord Gateway (WebSocket) : MESSAGE_CREATE 이벤트 발행
  -> internal/feature/ping/ping.go (onMessageCreate -> mentionsUser) : 멘션 확인 후 답장 전송
```
