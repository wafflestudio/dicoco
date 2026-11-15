# Dicoco

디스코드 Bot 레포

에이전트는 현재 비활성화되어 있으며 기본 이미지에 설치하지 않습니다. 재활성화 방법은 [운영 안내](docs/agent.md)를 참고하세요.

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

## 운영팀 TODO 리마인더

`cmd/ops-reminder`는 Cron에서 한 번 실행하고 종료하는 명령입니다. 기본 `-mode daily`는 회의 날짜 탐색, 다음 회의록 TODO 작성, Discord 근거로 완료 상태 갱신, 회의·마감 전날 알림을 순서대로 실행합니다. `-mode notion-sync`는 다음 회의록 TODO 생성까지만 실행합니다. `-send`가 없으면 쓰기 없는 미리보기입니다. 운영 스케줄은 매일 10:00 KST를 예정하며 아직 배포되지 않았습니다. 판정·발송 기준은 [운영팀 TODO 일일 파이프라인](docs/ops-todo-reminder.md)에 정리되어 있습니다.
