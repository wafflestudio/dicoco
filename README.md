# Dicoco

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
    └── reference/          구현 참고용 봇 기능
        ├── dm/             DM 수신 및 응답 확인
        ├── onreaction/     리액션 처리 확인
        └── ping/           멘션을 통한 봇 응답 확인
```

## Structure (멘션 ping 예시)

```text
cmd/bot/main.go (main)
  -> internal/app/app.go (app.Run)
       ├─> internal/config/config.go (config.Load) : 봇 토큰 로드
       ├─> internal/discord/client.go (discord.NewClient) : Intents 설정 및 세션 초기화
       ├─> internal/feature/reference/ping/ping.go (ping.New / Register) : onMessageCreate 핸들러 등록
       └─> internal/discord/client.go (discordClient.Open) : 디스코드 웹소켓 연결 시작

  [ 유저가 디스코드에 메시지 게시 ]

  -> Discord Gateway (WebSocket) : MESSAGE_CREATE 이벤트 발행
  -> internal/feature/reference/ping/ping.go (onMessageCreate -> mentionsUser) : 멘션 확인 후 답장 전송
```
