# Dicoco

디스코드 Bot 레포

## OpenCode 에이전트

지정 채널이나 그 안의 기존 스레드에서 봇을 멘션하면 코드 조사와 후속 대화를 진행합니다.
봇에게 보낸 DM은 멘션 없이 받으며, DM마다 별도 대화 세션을 사용합니다.
Go 봇이 같은 컨테이너 안에서 OpenCode 2를 실행하고 로컬 HTTP API로 연결합니다.

필수 설정, 권한, 실행 방법과 메모리 로그는 [에이전트 운영 안내](docs/agent.md)를 참고하세요.
`AGENT_CHANNEL_ID`를 설정하지 않으면 기존 봇 기능만 실행합니다.

## Requirements

- Go 1.27.1
- Discord bot token

Discord Developer Portal에서 봇의 **Message Content Intent**를 활성화해야 합니다.
`!사다리 @역할`로 접속하지 않은 역할 구성원까지 조회하려면 **Server Members Intent**도 활성화해야 합니다.

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
